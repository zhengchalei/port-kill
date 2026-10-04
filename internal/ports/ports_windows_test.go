package ports

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestDecodeTables(t *testing.T) {
	for _, spec := range []struct {
		family                       uint32
		tcp                          bool
		width, portOffset, pidOffset int
	}{{2, true, 24, 8, 20}, {23, true, 56, 20, 52}, {2, false, 12, 4, 8}, {23, false, 28, 20, 24}} {
		data := make([]byte, 4+spec.width)
		binary.LittleEndian.PutUint32(data, 1)
		row := data[4:]
		binary.BigEndian.PutUint16(row[spec.portOffset:], 8787)
		binary.LittleEndian.PutUint32(row[spec.pidOffset:], 4321)
		if spec.tcp {
			offset := 0
			if spec.family == 23 {
				offset = 48
			}
			binary.LittleEndian.PutUint32(row[offset:], 2)
		}
		rows, err := decodeTable(data, spec.family, spec.tcp)
		if err != nil || len(rows) != 1 || rows[0].Port != 8787 || rows[0].PID != 4321 {
			t.Fatalf("decode %+v: %+v, %v", spec, rows, err)
		}
		if spec.tcp && rows[0].State != "LISTEN" {
			t.Fatal(rows[0].State)
		}
		if _, err := decodeTable(data[:len(data)-1], spec.family, spec.tcp); err == nil {
			t.Fatal("truncated table accepted")
		}
	}
}

func TestPortHelper(t *testing.T) {
	if os.Getenv("PORTPILOT_TEST_CHILD") != "1" {
		return
	}
	tcp, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer udp.Close()
	json.NewEncoder(os.Stdout).Encode([]int{tcp.Addr().(*net.TCPAddr).Port, udp.LocalAddr().(*net.UDPAddr).Port})
	time.Sleep(time.Minute)
}

// Only this test's disposable child is terminated; existing user processes
// are never used as test targets.
func TestListAndKillOwnedChild(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestPortHelper$")
	command.Env = append(os.Environ(), "PORTPILOT_TEST_CHILD=1")
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	var bound []int
	if err = json.NewDecoder(output).Decode(&bound); err != nil {
		t.Fatal(err)
	}
	rows, err := List()
	if err != nil {
		t.Fatal(err)
	}
	var tcp Entry
	foundUDP := false
	for _, row := range rows {
		if row.PID != uint32(command.Process.Pid) {
			continue
		}
		if int(row.Port) == bound[0] && row.Protocol == "TCP" {
			tcp = row
		}
		if int(row.Port) == bound[1] && row.Protocol == "UDP" {
			foundUDP = true
		}
	}
	if tcp.Identity == "" || tcp.Protected || !foundUDP {
		t.Fatalf("missing child ports: TCP=%+v UDP=%v", tcp, foundUDP)
	}
	request := KillRequest{PID: tcp.PID, Identity: "stale", Protocol: tcp.Protocol, Address: tcp.Address, Port: tcp.Port}
	if Kill(request) == nil {
		t.Fatal("stale identity accepted")
	}
	request.Identity = tcp.Identity
	request.Port = 0
	if Kill(request) == nil {
		t.Fatal("unowned port accepted")
	}
	request.Port = tcp.Port
	if err = Kill(request); err != nil {
		t.Fatal(err)
	}
	command.Wait()
	listener, err := net.Listen("tcp4", tcp.Address+":"+strconv.Itoa(int(tcp.Port)))
	if err != nil {
		t.Fatalf("TCP not released: %v", err)
	}
	listener.Close()
	packet, err := net.ListenPacket("udp4", "127.0.0.1:"+strconv.Itoa(bound[1]))
	if err != nil {
		t.Fatalf("UDP not released: %v", err)
	}
	packet.Close()
	if Kill(KillRequest{PID: 4, Identity: "system"}) == nil {
		t.Fatal("system process accepted")
	}
}
