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
          const { userOptions = {} } = await chrome.storage.local.get('userOptions');
          const response = await chrome.runtime.sendMessage({
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
});

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
