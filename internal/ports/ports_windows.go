package ports

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

type Entry struct {
	PID       uint32 `json:"pid"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Identity  string `json:"identity"`
	Protocol  string `json:"protocol"`
	Address   string `json:"address"`
	Port      uint16 `json:"port"`
	Remote    string `json:"remote"`
	State     string `json:"state"`
	Protected bool   `json:"protected"`
}
type KillRequest struct {
	PID      uint32 `json:"pid"`
	Identity string `json:"identity"`
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Port     uint16 `json:"port"`
}

var iphelper = windows.NewLazySystemDLL("iphlpapi.dll")
var tcpTable = iphelper.NewProc("GetExtendedTcpTable")
var udpTable = iphelper.NewProc("GetExtendedUdpTable")
var isCritical = windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessCritical")

// Native owner-PID tables include IPv4 and IPv6 without spawning a shell.
func readTable(proc *windows.LazyProc, family, class uint32) ([]byte, error) {
	var size uint32
	code, _, _ := proc.Call(0, uintptr(unsafe.Pointer(&size)), 0, uintptr(family), uintptr(class), 0)
	if code != 0 && code != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
		return nil, windows.Errno(code)
	}
	for range 5 {
		if size < 4 {
			size = 4
		}
		data := make([]byte, size)
		code, _, _ = proc.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)), 0, uintptr(family), uintptr(class), 0)
		if code == 0 {
			return data, nil
		}
		if code != uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			return nil, windows.Errno(code)
		}
	}
	return nil, errors.New("端口表持续变化，请重试")
}
func decodeTable(data []byte, family uint32, tcp bool) ([]Entry, error) {
	width := 12
	if tcp {
		width = 24
	}
	if family == 23 {
		width = 28
		if tcp {
			width = 56
		}
	}
	if len(data) < 4 {
		return nil, errors.New("无效端口表")
	}
	count := int(binary.LittleEndian.Uint32(data))
	if count > (len(data)-4)/width {
		return nil, errors.New("端口表不完整")
	}
	rows := make([]Entry, 0, count)
	for i := range count {
		row := data[4+i*width : 4+(i+1)*width]
		entry := Entry{Protocol: "UDP", State: "BOUND"}
		addressOffset, portOffset, pidOffset := 0, 4, 8
		if tcp {
			entry.Protocol = "TCP"
			addressOffset, portOffset, pidOffset = 4, 8, 20
			entry.State = tcpState(binary.LittleEndian.Uint32(row))
		}
		if family == 23 {
			addressOffset, portOffset, pidOffset = 0, 20, 24
			if tcp {
				pidOffset = 52
				entry.State = tcpState(binary.LittleEndian.Uint32(row[48:]))
				entry.Remote = endpoint(row[24:40], binary.LittleEndian.Uint32(row[40:]), binary.BigEndian.Uint16(row[44:]))
			}
		}
		if family == 2 {
			entry.Address = net.IP(row[addressOffset : addressOffset+4]).String()
			if tcp {
				entry.Remote = endpoint(row[12:16], 0, binary.BigEndian.Uint16(row[16:]))
			}
		} else {
			entry.Address = scopedIP(row[:16], binary.LittleEndian.Uint32(row[16:]))
		}
		entry.Port = binary.BigEndian.Uint16(row[portOffset:])
		entry.PID = binary.LittleEndian.Uint32(row[pidOffset:])
		if entry.PID != 0 && entry.Port != 0 {
			rows = append(rows, entry)
		}
	}
	return rows, nil
}
func scopedIP(ip []byte, scope uint32) string {
	value := net.IP(ip).String()
	if scope != 0 {
		value += "%" + strconv.FormatUint(uint64(scope), 10)
	}
	return value
}
func endpoint(ip []byte, scope uint32, port uint16) string {
	if port == 0 {
		return "—"
	}
	return net.JoinHostPort(scopedIP(ip, scope), strconv.Itoa(int(port)))
}
func tcpState(state uint32) string {
	names := []string{"UNKNOWN", "CLOSED", "LISTEN", "SYN_SENT", "SYN_RECEIVED", "ESTABLISHED", "FIN_WAIT_1", "FIN_WAIT_2", "CLOSE_WAIT", "CLOSING", "LAST_ACK", "TIME_WAIT", "DELETE_TCB"}
	if int(state) < len(names) {
		return names[state]
	}
	return "UNKNOWN"
}
func creationIdentity(handle windows.Handle) (string, error) {
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return "", err
	}
	return strconv.FormatUint(uint64(created.HighDateTime)<<32|uint64(created.LowDateTime), 10), nil
}
func processNames() map[uint32]string {
	names := map[uint32]string{4: "System"}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return names
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	for err = windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		names[entry.ProcessID] = windows.UTF16ToString(entry.ExeFile[:])
	}
	return names
}
func List() ([]Entry, error) {
	rows := []Entry{}
	for _, spec := range []struct {
		family uint32
		tcp    bool
	}{{2, true}, {23, true}, {2, false}, {23, false}} {
		proc, class := udpTable, uint32(1)
		if spec.tcp {
			proc, class = tcpTable, 5
		}
		data, err := readTable(proc, spec.family, class)
		if err != nil {
			return nil, fmt.Errorf("读取端口失败: %w", err)
		}
		entries, err := decodeTable(data, spec.family, spec.tcp)
		if err != nil {
			return nil, err
		}
		rows = append(rows, entries...)
	}
	names := processNames()
	cache := map[uint32]Entry{}
	for i := range rows {
		id := rows[i].PID
		info, ok := cache[id]
		if !ok {
			info.Name = names[id]
			if info.Name == "" {
				info.Name = "进程已退出"
			}
			info.Protected = id <= 4 || id == uint32(os.Getpid())
			handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, id)
			if err == nil {
				info.Identity, _ = creationIdentity(handle)
				buf := make([]uint16, 32768)
				size := uint32(len(buf))
				if windows.QueryFullProcessImageName(handle, 0, &buf[0], &size) == nil {
					info.Path = windows.UTF16ToString(buf[:size])
					info.Name = filepath.Base(info.Path)
				}
				var critical uint32
				result, _, _ := isCritical.Call(uintptr(handle), uintptr(unsafe.Pointer(&critical)))
				info.Protected = info.Protected || result == 0 || critical != 0
				windows.CloseHandle(handle)
			} else {
				info.Protected = true
			}
			info.Protected = info.Protected || info.Identity == ""
			cache[id] = info
		}
		rows[i].Name, rows[i].Path, rows[i].Identity, rows[i].Protected = info.Name, info.Path, info.Identity, info.Protected
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Port != rows[j].Port {
			return rows[i].Port < rows[j].Port
		}
		if rows[i].PID != rows[j].PID {
			return rows[i].PID < rows[j].PID
		}
		return rows[i].Address < rows[j].Address
	})
	return rows, nil
}

func Kill(request KillRequest) error {
	if request.PID <= 4 || request.PID == uint32(os.Getpid()) || request.Identity == "" {
		return errors.New("不能结束系统进程或端口管理器自身")
	}
	// Hold the same process handle from identity validation through termination,
	// so a recycled PID cannot target a different process.
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, request.PID)
	if err != nil {
		return fmt.Errorf("无法结束进程，请检查权限或以管理员身份运行: %w", err)
	}
	defer windows.CloseHandle(handle)
	identity, err := creationIdentity(handle)
	if err != nil || identity != request.Identity {
		return errors.New("进程已变化，请刷新后重新选择")
	}
	var critical uint32
	result, _, _ := isCritical.Call(uintptr(handle), uintptr(unsafe.Pointer(&critical)))
	if result == 0 || critical != 0 {
		return errors.New("不能结束受保护的系统进程")
	}
	rows, err := List()
	if err != nil {
		return err
	}
	ownsPort := false
	for _, row := range rows {
		if row.PID == request.PID && row.Identity == request.Identity && row.Protocol == request.Protocol && row.Address == request.Address && row.Port == request.Port {
			ownsPort = true
			break
		}
	}
	if !ownsPort {
		return errors.New("该端口已不再由此进程占用，请刷新")
	}
	if err := windows.TerminateProcess(handle, 1); err != nil {
		return fmt.Errorf("结束失败: %w", err)
	}
	status, err := windows.WaitForSingleObject(handle, 5000)
	if err != nil {
		return err
	}
	if status != windows.WAIT_OBJECT_0 {
		return errors.New("结束请求已发送，但进程尚未退出，请刷新检查")
	}
	return nil
}
