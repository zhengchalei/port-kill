const $ = (id) => document.getElementById(id);
const state = { rows: [], selected: null, scope: 'listening', sort: 'port', direction: 1, busy: false, pending: null };
const desktop = !!window.mygo;
const esc = (value) => String(value ?? '').replace(/[&<>"']/g, (char) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
const key = (row) => `${row.pid}/${row.identity}/${row.protocol}/${row.address}/${row.port}/${row.remote}/${row.state}`;
const icon = (name) => `<svg aria-hidden="true"><use href="#${name}"/></svg>`;
function processIcon(row) { const name = row.name.toLowerCase(); const kind = /node|python|go|dotnet/.test(name) ? '' : /system|svchost|service/.test(name) ? 'blue' : 'purple'; return `<span class="process-icon ${kind}">${/node/.test(name) ? '⬡' : /python/.test(name) ? 'Py' : /system|svchost/.test(name) ? '⊞' : '›_'}</span>`; }
function toast(message) { $('toast').textContent = message; $('toast').hidden = false; clearTimeout(toast.timer); toast.timer = setTimeout(() => $('toast').hidden = true, 5000); }
function filteredRows() {
 const query = $('search-input').value.trim().toLowerCase();
 return state.rows.filter((row) => (state.scope === 'all' || row.protocol === 'UDP' || row.state === 'LISTEN') && ($('protocol').value === 'all' || row.protocol === $('protocol').value) && (!query || [row.name,row.pid,row.port,row.address,row.path].some((value) => String(value).toLowerCase().includes(query)))).sort((a,b) => { const x = a[state.sort], y = b[state.sort]; return (typeof x === 'number' ? x-y : String(x).localeCompare(String(y))) * state.direction || a.port-b.port; });
}
function render() {
 const rows = filteredRows();
 $('stat-ports').textContent = new Set(rows.map((row) => `${row.protocol}/${row.port}`)).size;
 $('stat-processes').textContent = new Set(rows.map((row) => row.pid)).size;
 $('stat-tcp').textContent = new Set(rows.filter((row) => row.protocol === 'TCP' && row.state === 'LISTEN').map((row) => row.port)).size;
 $('stat-udp').textContent = new Set(rows.filter((row) => row.protocol === 'UDP').map((row) => row.port)).size;
 $('nav-count').textContent = $('stat-ports').textContent;
 if (!rows.some((row) => key(row) === state.selected)) state.selected = rows.length ? key(rows.find((row) => !row.protected) || rows[0]) : null;
 $('rows').innerHTML = rows.map((row,index) => `<tr class="port-row ${key(row) === state.selected ? 'selected' : ''}" tabindex="0" aria-selected="${key(row) === state.selected}" data-index="${index}"><td><div class="process-name">${processIcon(row)}<span title="${esc(row.name)}">${esc(row.name)}</span></div></td><td>${row.pid}</td><td class="port-number">${row.port}</td><td><span class="badge ${row.protocol === 'UDP' ? 'udp' : ''}">${row.protocol}</span></td><td>${esc(row.address)}</td><td><span class="state ${row.state !== 'LISTEN' && row.state !== 'BOUND' ? 'other' : ''}">${row.state === 'LISTEN' ? '监听中' : row.state === 'BOUND' ? '已绑定' : esc(row.state)}</span></td></tr>`).join('');
 $('empty').hidden = rows.length > 0;
 $('row-count').textContent = `共 ${rows.length} 条端口记录 · ${new Set(rows.map((row) => row.pid)).size} 个进程`;
 $('rows').querySelectorAll('tr').forEach((tr) => { const select = () => { state.selected = key(rows[Number(tr.dataset.index)]); render(); }; tr.onclick = select; tr.onkeydown = (event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); select(); } }; });
 renderDetails();
}
function renderDetails() {
 const row = state.rows.find((row) => key(row) === state.selected);
 if (!row) { $('detail-body').innerHTML = '<div class="empty">选择一条端口记录<p>查看关联进程信息</p></div>'; return; }
 const siblings = state.rows.filter((entry) => entry.pid === row.pid && entry.identity === row.identity);
 const ports = [...new Set(siblings.map((entry) => entry.port))].sort((a,b) => a-b);
 $('detail-body').innerHTML = `<div class="detail-info"><div class="detail-hero">${processIcon(row)}<div><strong>${esc(row.name)}</strong><small>${row.protected ? '受保护进程' : '本地运行的进程'}</small></div></div><div class="detail-label">进程 ID</div><div class="detail-value">${row.pid}</div><div class="detail-label">可执行文件路径</div><div class="detail-value path">${esc(row.path || '无法读取 · 权限不足或进程已退出')}</div><button id="copy-path" class="copy-button" ${!row.path ? 'disabled' : ''}>${icon('copy')}复制路径</button><div class="detail-label">占用端口 <span>(${ports.length})</span></div><div class="port-chips">${ports.map((port) => `<span>${port}</span>`).join('')}</div><div class="detail-label">当前连接</div><div class="detail-value">${esc(row.protocol)} · ${esc(row.address)}:${row.port}<br>远程：${esc(row.remote || '—')}</div></div><div class="detail-divider"><button id="kill-btn" class="button kill-button" ${row.protected || !desktop ? 'disabled' : ''}>${icon(row.protected ? 'shield' : 'stop')}结束进程</button><p class="detail-note">${!desktop ? '浏览器预览为只读。请在桌面应用中结束进程。' : row.protected ? '系统关键进程、不可访问的进程和应用自身受保护。' : `结束进程将关闭它的全部连接，并释放 ${ports.length} 个端口。`}</p></div>`;
 $('copy-path').onclick = async () => { try { await navigator.clipboard.writeText(row.path); toast('路径已复制'); } catch { toast('无法访问剪贴板'); } };
 $('kill-btn').onclick = () => { state.pending = {...row}; $('kill-description').textContent = `${row.name} · PID ${row.pid}，占用端口 ${ports.join('、')}。`; $('kill-dialog').showModal(); };
}
async function refresh() {
 if (state.busy || $('kill-dialog').open) return;
 state.busy = true; $('refresh-btn').disabled = true;
 try {
  let rows;
  if (desktop) rows = await window.mygo.call('PortService.ListPorts');
  else { const response = await fetch('/api/ports', {cache:'no-store'}); if (!response.ok) throw new Error(await response.text()); rows = await response.json(); }
  state.rows = rows || []; render(); $('updated').textContent = `${new Date().toLocaleTimeString('zh-CN',{hour12:false})} 更新`;
  $('connection').textContent = desktop ? '已连接本机' : '本机 · 只读预览';
 } catch (error) { $('connection').textContent = '读取失败'; toast(`读取端口失败：${error.message || error}`); }
 finally { state.busy = false; $('refresh-btn').disabled = false; }
}
$('refresh-btn').onclick = refresh;
$('search-input').oninput = render;
$('protocol').onchange = render;
document.querySelectorAll('[data-scope]').forEach((button) => button.onclick = () => { state.scope = button.dataset.scope; document.querySelectorAll('[data-scope]').forEach((tab) => { const selected = tab === button; tab.classList.toggle('selected',selected); tab.setAttribute('aria-selected',String(selected)); }); render(); });
document.querySelectorAll('[data-sort]').forEach((button) => button.onclick = () => { state.direction = state.sort === button.dataset.sort ? -state.direction : 1; state.sort = button.dataset.sort; document.querySelectorAll('[data-sort] span').forEach((span) => span.textContent = '↕'); button.querySelector('span').textContent = state.direction === 1 ? '↑' : '↓'; render(); });
$('cancel-kill').onclick = () => { state.pending = null; $('kill-dialog').close(); };
$('kill-dialog').addEventListener('cancel',() => state.pending = null);
$('confirm-kill').onclick = async () => {
 if (!state.pending || !desktop) return;
 const row = state.pending; $('confirm-kill').disabled = true; $('cancel-kill').disabled = true;
 try { await window.mygo.call('PortService.KillProcess',{pid:row.pid,identity:row.identity,protocol:row.protocol,address:row.address,port:row.port}); toast(`已结束 ${row.name}（PID ${row.pid}）`); }
 catch (error) { toast(`结束失败：${error.message || error}`); }
 finally { state.pending = null; $('kill-dialog').close(); $('confirm-kill').disabled = false; $('cancel-kill').disabled = false; await refresh(); }
};
document.addEventListener('keydown',(event) => { if (event.key === 'F5') { event.preventDefault(); refresh(); } if (event.key === '/' && !['INPUT','SELECT','TEXTAREA'].includes(document.activeElement.tagName) && !$('kill-dialog').open) { event.preventDefault(); $('search-input').focus(); } });
setInterval(() => { if ($('auto-refresh').checked && !document.hidden) refresh(); },5000);
refresh();
