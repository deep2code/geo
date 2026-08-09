const endpointEl = document.getElementById('endpoint');
const tokenEl = document.getElementById('token');
const brandNameEl = document.getElementById('brandName');
const togglePwBtn = document.getElementById('togglePw');
const saveBtn = document.getElementById('saveBtn');
const testBtn = document.getElementById('testBtn');
const statusEl = document.getElementById('status');
const testResultEl = document.getElementById('testResult');
const brandHeader = document.getElementById('brandHeader');

togglePwBtn.addEventListener('click', () => {
  if (tokenEl.type === 'password') {
    tokenEl.type = 'text';
    togglePwBtn.textContent = '隐藏';
  } else {
    tokenEl.type = 'password';
    togglePwBtn.textContent = '显示';
  }
});

async function loadOptions() {
  const stored = await chrome.storage.local.get('userOptions');
  const opts = stored.userOptions || {};
  endpointEl.value = opts.geoEndpoint || '';
  tokenEl.value = opts.token || '';
  brandNameEl.value = opts.brandName || '';
  if (opts.brandName) {
    brandHeader.textContent = `${opts.brandName} - 扩展设置`;
    document.title = `${opts.brandName} - 选项设置`;
  }
}

saveBtn.addEventListener('click', async () => {
  const endpoint = endpointEl.value.trim();
  const token = tokenEl.value.trim();
  const brandName = brandNameEl.value.trim();

  if (!endpoint) {
    showStatus('请填写 GEO Endpoint', 'error');
    endpointEl.focus();
    return;
  }

  try {
    new URL(endpoint);
  } catch {
    showStatus('Endpoint 格式不正确，应以 http:// 或 https:// 开头', 'error');
    endpointEl.focus();
    return;
  }

  saveBtn.disabled = true;
  saveBtn.textContent = '保存中...';

  try {
    await chrome.storage.local.set({
      userOptions: {
        geoEndpoint: endpoint,
        token,
        brandName
      }
    });
    showStatus('✅ 设置已保存', 'success');
    if (brandName) {
      brandHeader.textContent = `${brandName} - 扩展设置`;
      document.title = `${brandName} - 选项设置`;
    } else {
      brandHeader.textContent = 'GEO 分析扩展设置';
      document.title = 'GEO 分析 - 选项设置';
    }
  } catch (err) {
    showStatus('保存失败: ' + (err.message || err), 'error');
  } finally {
    saveBtn.disabled = false;
    saveBtn.textContent = '保存设置';
  }
});

testBtn.addEventListener('click', async () => {
  const endpoint = endpointEl.value.trim();
  const token = tokenEl.value.trim();

  testResultEl.className = 'test-result';
  testResultEl.textContent = '';

  if (!endpoint) {
    showTestResult('请先填写 Endpoint', false);
    return;
  }

  testBtn.disabled = true;
  const originalText = testBtn.textContent;
  testBtn.textContent = '测试中...';

  try {
    new URL(endpoint);
  } catch {
    showTestResult('Endpoint 格式不正确', false);
    testBtn.disabled = false;
    testBtn.textContent = originalText;
    return;
  }

  try {
    const apiUrl = endpoint.replace(/\/+$/, '') + '/api/v1/cms/check';
    const headers = { 'Content-Type': 'application/json' };
    if (token) headers['Authorization'] = `Bearer ${token}`;

    const res = await fetch(apiUrl, {
      method: 'POST',
      headers,
      body: JSON.stringify({
        url: 'chrome-extension://test',
        title: 'Connection Test',
        html: '<p>test</p>',
        plainText: 'test',
        mode: 'healthcheck'
      })
    });

    if (res.ok) {
      let info = `连接成功 (HTTP ${res.status})`;
      try {
        const data = await res.json();
        if (data.score !== undefined) info += `，返回评分字段 score = ${data.score}`;
      } catch {}
      showTestResult(info, true);
    } else {
      showTestResult(`服务器返回错误：HTTP ${res.status} ${res.statusText}`, false);
    }
  } catch (err) {
    showTestResult(`请求失败：${err.message || err}（可能是跨域/CORS 或网络问题）`, false);
  } finally {
    testBtn.disabled = false;
    testBtn.textContent = originalText;
  }
});

function showStatus(msg, type) {
  statusEl.textContent = msg;
  statusEl.className = 'status ' + (type === 'error' ? 'status-error' : type === 'success' ? 'status-success' : '');
  if (type === 'success') {
    setTimeout(() => {
      if (statusEl.textContent === msg) statusEl.textContent = '';
    }, 3000);
  }
}

function showTestResult(msg, ok) {
  testResultEl.className = 'test-result show ' + (ok ? 'test-success' : 'test-fail');
  testResultEl.textContent = (ok ? '✅ ' : '❌ ') + msg;
}

loadOptions();
