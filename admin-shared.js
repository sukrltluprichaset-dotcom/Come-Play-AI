// ================= ค่ากลางที่ใช้ร่วมกันทุกหน้า Admin =================
const API_BASE_URL = 'https://come-play-ai-1.onrender.com';

async function apiFetch(path, options = {}) {
    const token = localStorage.getItem('token');
    const headers = { 'Content-Type': 'application/json', ...(options.headers || {}) };
    if (token) headers['Authorization'] = 'Bearer ' + token;
    const response = await fetch(API_BASE_URL + path, { ...options, headers });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.error || 'เกิดข้อผิดพลาด');
    return data;
}

// เรียกทุกหน้า (ยกเว้น admin-login.html) ตอนโหลดหน้า เพื่อเช็คว่ามีสิทธิ์ admin จริงไหม
// ถ้าไม่ผ่าน จะเด้งกลับไปหน้า login อัตโนมัติ
function requireAdminAuth() {
    const token = localStorage.getItem('token');
    const user = JSON.parse(localStorage.getItem('user') || 'null');
    if (!token || !user || user.role !== 'admin') {
        window.location.href = 'admin-login.html';
        return null;
    }
    return user;
}

function adminLogout() {
    localStorage.removeItem('token');
    localStorage.removeItem('user');
    window.location.href = 'admin-login.html';
}

// แสดง popup แจ้งเตือนแบบง่าย ใช้ร่วมกันทุกหน้า
function adminAlert(message) {
    let modal = document.getElementById('admin-alert-modal');
    if (!modal) {
        document.body.insertAdjacentHTML('beforeend', `
            <div id="admin-alert-modal" class="fixed inset-0 z-[100] hidden flex items-center justify-center bg-slate-900/70 backdrop-blur-sm">
                <div class="bg-white rounded-2xl shadow-2xl p-6 max-w-sm w-full mx-4 text-center">
                    <p id="admin-alert-message" class="text-slate-700 mb-4"></p>
                    <button onclick="document.getElementById('admin-alert-modal').classList.add('hidden')" class="w-full bg-indigo-600 hover:bg-indigo-700 text-white font-bold py-2.5 rounded-lg transition">ตกลง</button>
                </div>
            </div>`);
        modal = document.getElementById('admin-alert-modal');
    }
    document.getElementById('admin-alert-message').innerText = message;
    modal.classList.remove('hidden');
}

// สร้างแถบเมนูบนหัวหน้า ใช้ร่วมกันทุกหน้า (ยกเว้นหน้า login)
// activePage: 'dashboard' | 'users' | 'characters' | 'reports'
function renderAdminNav(activePage, username) {
    const navItems = [
        { key: 'dashboard', label: 'ภาพรวม', icon: 'fa-chart-simple', href: 'admin-dashboard.html' },
        { key: 'users', label: 'ผู้ใช้งาน', icon: 'fa-users', href: 'admin-users.html' },
        { key: 'characters', label: 'ตัวละคร', icon: 'fa-robot', href: 'admin-characters.html' },
        { key: 'reports', label: 'รายงาน', icon: 'fa-flag', href: 'admin-reports.html' },
    ];

    const navHTML = `
        <header class="bg-indigo-900 text-white px-6 py-4 flex justify-between items-center shadow-lg">
            <div class="flex items-center space-x-3">
                <i class="fa-solid fa-user-shield text-2xl text-indigo-300"></i>
                <div>
                    <h1 class="font-bold text-lg leading-tight">Admin Dashboard</h1>
                    <p class="text-xs text-indigo-300">Come Play AI</p>
                </div>
            </div>
            <div class="flex items-center space-x-4">
                <span class="text-sm text-indigo-200">${username || ''}</span>
                <button onclick="adminLogout()" class="text-red-300 hover:text-red-100 transition p-2 rounded-full hover:bg-indigo-800" title="ออกจากระบบ">
                    <i class="fa-solid fa-right-from-bracket"></i>
                </button>
            </div>
        </header>
        <div class="max-w-6xl mx-auto px-6 pt-6">
            <div class="flex space-x-2 mb-6 bg-white p-2 rounded-xl shadow-sm border border-slate-200 overflow-x-auto">
                ${navItems.map(item => `
                    <a href="${item.href}" class="px-4 py-2 rounded-lg font-bold text-sm transition whitespace-nowrap ${item.key === activePage ? 'bg-indigo-600 text-white' : 'text-slate-600 hover:bg-slate-100'}">
                        <i class="fa-solid ${item.icon} mr-2"></i>${item.label}
                    </a>`).join('')}
            </div>
        </div>`;

    document.getElementById('admin-nav-container').innerHTML = navHTML;
}