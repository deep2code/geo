const CONTEXT_MENU_ID = 'geo-analyze-page';

chrome.runtime.onInstalled.addListener(() => {
  chrome.contextMenus.create({
    id: CONTEXT_MENU_ID,
    title: 'GEO 分析此页',
    contexts: ['page', 'selection']
  });
});

chrome.action.onClicked.addListener(async (tab) => {
  try {
    await chrome.sidePanel.open({ windowId: tab.windowId });
  } catch (error) {
    console.error('Failed to open side panel:', error);
  }
});

chrome.contextMenus.onClicked.addListener(async (info, tab) => {
  if (info.menuItemId === CONTEXT_MENU_ID && tab?.id) {
    try {
      await chrome.sidePanel.open({ windowId: tab.windowId });
      setTimeout(async () => {
        try {
          await chrome.runtime.sendMessage({
            type: 'ANALYZE_PAGE',
            tabId: tab.id,
            url: tab.url,
            title: tab.title,
            selectionText: info.selectionText || null
          });
        } catch (e) {
          console.log('Side panel may not be ready yet, will retry on load');
        }
      }, 300);
    } catch (error) {
      console.error('Failed to handle context menu:', error);
    }
  }
});

chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
  if (message.type === 'EXTRACT_AND_ANALYZE') {
    handleExtractAndAnalyze(message, sender)
      .then(result => sendResponse({ success: true, data: result }))
      .catch(error => sendResponse({ success: false, error: error.message }));
    return true;
  }
  if (message.type === 'ANALYZE_SELECTION') {
    handleAnalyzeSelection(message)
      .then(result => sendResponse({ success: true, data: result }))
      .catch(error => sendResponse({ success: false, error: error.message }));
    return true;
  }
});

// 选中文本分析：由 content script 委托发请求。content script 的 fetch 遵循
// 宿主页面的 CSP（connect-src），在配置了严格 CSP 的站点上会被拦截；
// service worker 的 fetch 走扩展 host_permissions，不受页面 CSP 约束。
async function handleAnalyzeSelection(message) {
  const { url, title, html, plainText } = message;
  const { userOptions = {} } = await chrome.storage.local.get('userOptions');
  const { geoEndpoint, token } = userOptions;

  if (!geoEndpoint) {
    throw new Error('请先在扩展选项中配置 GEO Endpoint');
  }

  const headers = {
    'Content-Type': 'application/json'
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const apiUrl = geoEndpoint.replace(/\/+$/, '') + '/api/v1/cms/check';

  const response = await fetch(apiUrl, {
    method: 'POST',
    headers,
    body: JSON.stringify({
      url,
      title,
      html,
      plainText,
      mode: 'selection'
    })
  });

  if (!response.ok) {
    throw new Error(`API 错误 ${response.status}: ${response.statusText}`);
  }

  return await response.json();
}

async function handleExtractAndAnalyze(message, sender) {
  const { tabId, url, title } = message;
  const { userOptions = {} } = await chrome.storage.local.get('userOptions');
  const { geoEndpoint, token } = userOptions;

  if (!geoEndpoint) {
    throw new Error('请先在选项中配置 GEO Endpoint');
  }

  const [{ result }] = await chrome.scripting.executeScript({
    target: { tabId },
    func: extractPageContent
  });

  const body = JSON.stringify({
    url,
    title,
    content: result.html,
    plainText: result.plainText
  });

  const headers = {
    'Content-Type': 'application/json'
  };

  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const apiUrl = geoEndpoint.replace(/\/+$/, '') + '/api/v1/cms/check';

  const response = await fetch(apiUrl, {
    method: 'POST',
    headers,
    body
  });

  if (!response.ok) {
    throw new Error(`API 请求失败: ${response.status} ${response.statusText}`);
  }

  return await response.json();
}

function extractPageContent() {
  const articleSelectors = [
    'article',
    'main',
    '[role="main"]',
    '.article-body',
    '.post-content',
    '.content',
    '.entry-content',
    '#content',
    '#main-content'
  ];

  let html = '';
  let plainText = '';

  for (const selector of articleSelectors) {
    const el = document.querySelector(selector);
    if (el) {
      html = el.innerHTML;
      plainText = el.innerText || el.textContent || '';
      break;
    }
  }

  if (!html) {
    html = document.body.innerHTML;
    plainText = document.body.innerText || document.body.textContent || '';
  }

  return { html, plainText: plainText.trim() };
}
