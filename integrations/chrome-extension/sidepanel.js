const brandNameEl = document.getElementById('brandName');
const settingsBtn = document.getElementById('settingsBtn');
const analyzeBtn = document.getElementById('analyzeBtn');
const contentEl = document.getElementById('content');
const configureLink = document.getElementById('configureLink');

let userOptions = {
  geoEndpoint: '',
  token: '',
  brandName: ''
};

async function init() {
  const stored = await chrome.storage.local.get('userOptions');
  userOptions = { ...userOptions, ...(stored.userOptions || {}) };

  if (userOptions.brandName) {
    brandNameEl.textContent = userOptions.brandName;
    document.title = `${userOptions.brandName} - 分析`;
  }
}

settingsBtn.addEventListener('click', () => {
  chrome.runtime.openOptionsPage();
});

configureLink.addEventListener('click', () => {
  chrome.runtime.openOptionsPage();
});

analyzeBtn.addEventListener('click', () => {
  analyzeCurrentPage();
});

async function analyzeCurrentPage() {
  if (!userOptions.geoEndpoint) {
    renderError('请先在设置中配置 GEO Endpoint');
    return;
  }

  renderLoading();

  try {
    const [tab] = await chrome.tabs.query({ active: true, currentWindow: true });
    if (!tab?.id) {
      renderError('无法获取当前标签页');
      return;
    }

    const response = await chrome.runtime.sendMessage({
      type: 'EXTRACT_AND_ANALYZE',
      tabId: tab.id,
      url: tab.url,
      title: tab.title
    });

    if (!response.success) {
      renderError(response.error || '分析失败');
      return;
    }

    renderResult(tab, response.data);
  } catch (error) {
    renderError(error.message || '发生未知错误');
  }
}

function renderLoading() {
  contentEl.innerHTML = `
    <div class="empty-state">
      <div class="empty-icon">⏳</div>
      <div class="empty-title"><span class="loading"></span>正在分析页面...</div>
      <div class="empty-desc">正在提取内容并调用 GEO API</div>
    </div>
  `;
}

function renderError(message) {
  contentEl.innerHTML = `
    <div class="error-state">
      <div class="empty-icon">⚠️</div>
      <div class="empty-title error-title">分析失败</div>
      <div class="empty-desc">${escapeHtml(message)}</div>
      <button class="btn" id="retryBtn">重试</button>
    </div>
  `;
  document.getElementById('retryBtn').addEventListener('click', analyzeCurrentPage);
}

function renderResult(tab, data) {
  const score = data.score ?? data.overallScore ?? 0;
  const suggestions = data.suggestions || data.issues || [];

  const scoreClass = score >= 80 ? 'score-good' : score >= 60 ? 'score-warn' : 'score-bad';

  let suggestionsHtml = '';
  if (suggestions.length > 0) {
    suggestionsHtml = '<div class="suggestions-title">优化建议</div>';
    suggestionsHtml += suggestions.map(s => {
      const type = s.severity || s.level || s.type || 'info';
      const typeLabel = {
        error: '错误',
        warning: '警告',
        warn: '警告',
        good: '良好',
        success: '良好',
        info: '提示'
      }[type] || '提示';
      const typeClass = {
        error: 'type-error',
        warning: 'type-warn',
        warn: 'type-warn',
        good: 'type-good',
        success: 'type-good',
        info: 'type-info'
      }[type] || 'type-info';
      const itemClass = {
        error: 'suggestion-error',
        warning: 'suggestion-warn',
        warn: 'suggestion-warn',
        good: 'suggestion-good',
        success: 'suggestion-good'
      }[type] || 'suggestion-item';
      const text = s.message || s.text || s.description || s.title || JSON.stringify(s);
      return `
        <div class="suggestion-item ${itemClass}">
          <span class="suggestion-type ${typeClass}">${typeLabel}</span>
          <div class="suggestion-text">${escapeHtml(text)}</div>
        </div>
      `;
    }).join('');
  }

  contentEl.innerHTML = `
    <div class="info-card">
      <div class="info-title">页面标题</div>
      <div class="info-value">${escapeHtml(tab.title || '(无标题)')}</div>
    </div>
    <div class="info-card">
      <div class="info-title">页面 URL</div>
      <div class="info-value">${escapeHtml(tab.url)}</div>
    </div>
    <div class="score-section">
      <div class="score-label">GEO 质量评分</div>
      <div class="score-value ${scoreClass}">${Math.round(score)}</div>
      <div style="margin-top:8px;font-size:12px;color:#909399">
        ${score >= 80 ? '✨ 内容质量优秀' : score >= 60 ? '💡 还有优化空间' : '⚠️ 需要重点改进'}
      </div>
    </div>
    ${suggestionsHtml}
  `;
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = String(str);
  return div.innerHTML;
}

init();
