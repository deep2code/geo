(function () {
  if (window.__geoTextAnalyzerInjected) return;
  window.__geoTextAnalyzerInjected = true;

  let floatingBtn = null;
  let currentSelection = null;
  let popupEl = null;

  function createFloatingButton() {
    if (floatingBtn && document.body.contains(floatingBtn)) {
      return floatingBtn;
    }

    floatingBtn = document.createElement('button');
    floatingBtn.id = 'geo-floating-btn';
    floatingBtn.textContent = 'GEO 文本分析';
    Object.assign(floatingBtn.style, {
      position: 'fixed',
      zIndex: 2147483647,
      background: 'linear-gradient(135deg, #1a73e8, #4285f4)',
      color: '#fff',
      border: 'none',
      borderRadius: '20px',
      padding: '6px 14px',
      fontSize: '12px',
      fontWeight: 500,
      cursor: 'pointer',
      boxShadow: '0 2px 8px rgba(26, 115, 232, 0.35)',
      display: 'none',
      fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
      transition: 'transform 0.15s, opacity 0.15s',
      userSelect: 'none'
    });

    floatingBtn.addEventListener('mouseenter', () => {
      floatingBtn.style.transform = 'translateY(-1px)';
    });
    floatingBtn.addEventListener('mouseleave', () => {
      floatingBtn.style.transform = 'translateY(0)';
    });
    floatingBtn.addEventListener('click', (e) => {
      e.preventDefault();
      e.stopPropagation();
      handleAnalyzeSelection();
    });

    document.body.appendChild(floatingBtn);
    return floatingBtn;
  }

  function createPopup() {
    if (popupEl && document.body.contains(popupEl)) {
      return popupEl;
    }

    popupEl = document.createElement('div');
    popupEl.id = 'geo-popup';
    Object.assign(popupEl.style, {
      position: 'fixed',
      zIndex: 2147483647,
      width: '360px',
      maxWidth: 'calc(100vw - 32px)',
      maxHeight: '520px',
      overflowY: 'auto',
      background: '#fff',
      borderRadius: '10px',
      boxShadow: '0 8px 32px rgba(0,0,0,0.18)',
      display: 'none',
      fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
      color: '#303133',
      fontSize: '13px'
    });

    document.body.appendChild(popupEl);
    return popupEl;
  }

  function showFloatingButton(rect) {
    const btn = createFloatingButton();
    btn.style.display = 'block';
    btn.style.opacity = '1';

    const btnWidth = 110;
    const scrollTop = window.scrollY || document.documentElement.scrollTop;
    const scrollLeft = window.scrollX || document.documentElement.scrollLeft;

    let top = rect.top + scrollTop - 42;
    let left = rect.left + scrollLeft + (rect.width - btnWidth) / 2;

    if (top < scrollTop + 8) {
      top = rect.bottom + scrollTop + 8;
    }

    const maxLeft = scrollLeft + window.innerWidth - btnWidth - 8;
    left = Math.max(scrollLeft + 8, Math.min(left, maxLeft));

    btn.style.top = `${top}px`;
    btn.style.left = `${left}px`;
  }

  function hideFloatingButton() {
    if (floatingBtn) {
      floatingBtn.style.opacity = '0';
      setTimeout(() => {
        if (floatingBtn) floatingBtn.style.display = 'none';
      }, 150);
    }
  }

  document.addEventListener('mouseup', (e) => {
    if (popupEl && popupEl.contains(e.target)) return;
    if (floatingBtn && floatingBtn.contains(e.target)) return;

    setTimeout(() => {
      const selection = window.getSelection();
      const text = selection ? selection.toString().trim() : '';

      if (text.length > 0) {
        currentSelection = {
          text,
          html: getSelectionHtml(selection)
        };
        const range = selection.getRangeAt(0);
        const rect = range.getBoundingClientRect();
        if (rect.width > 0 || rect.height > 0) {
          showFloatingButton(rect);
        }
      } else {
        currentSelection = null;
        hideFloatingButton();
        hidePopup();
      }
    }, 10);
  });

  document.addEventListener('mousedown', (e) => {
    if (popupEl && popupEl.contains(e.target)) return;
    if (floatingBtn && floatingBtn.contains(e.target)) return;
    hidePopup();
  });

  function getSelectionHtml(selection) {
    if (!selection || selection.rangeCount === 0) return '';
    const container = document.createElement('div');
    for (let i = 0; i < selection.rangeCount; i++) {
      container.appendChild(selection.getRangeAt(i).cloneContents());
    }
    return container.innerHTML;
  }

  async function handleAnalyzeSelection() {
    if (!currentSelection) return;
    hideFloatingButton();
    showLoadingPopup();

    try {
      const stored = await chrome.storage.local.get('userOptions');
      const { userOptions = {} } = stored;
      const { geoEndpoint, token } = userOptions;

      if (!geoEndpoint) {
        showErrorPopup('请先在扩展选项中配置 GEO Endpoint');
        return;
      }

      const apiUrl = geoEndpoint.replace(/\/+$/, '') + '/api/v1/cms/check';

      const headers = {
        'Content-Type': 'application/json'
      };
      if (token) {
        headers['Authorization'] = `Bearer ${token}`;
      }

      const body = JSON.stringify({
        url: location.href,
        title: document.title,
        html: currentSelection.html,
        plainText: currentSelection.text,
        mode: 'selection'
      });

      const response = await fetch(apiUrl, {
        method: 'POST',
        headers,
        body
      });

      if (!response.ok) {
        throw new Error(`API 错误 ${response.status}: ${response.statusText}`);
      }

      const data = await response.json();
      showResultPopup(data, currentSelection.text);
    } catch (err) {
      showErrorPopup(err.message || '请求失败');
    }
  }

  function positionPopupNearSelection() {
    const popup = createPopup();
    const selection = window.getSelection();
    let top, left;

    if (selection && selection.rangeCount > 0) {
      const rect = selection.getRangeAt(0).getBoundingClientRect();
      const scrollTop = window.scrollY || document.documentElement.scrollTop;
      const scrollLeft = window.scrollX || document.documentElement.scrollLeft;

      top = rect.bottom + scrollTop + 8;
      left = rect.left + scrollLeft;

      const popupWidth = 360;
      const maxLeft = scrollLeft + window.innerWidth - popupWidth - 16;
      left = Math.max(scrollLeft + 8, Math.min(left, maxLeft));

      const popupHeight = 400;
      if (top + popupHeight > scrollTop + window.innerHeight - 8) {
        top = rect.top + scrollTop - popupHeight - 8;
      }
    } else {
      top = (window.scrollY || 0) + window.innerHeight / 2 - 200;
      left = (window.scrollX || 0) + window.innerWidth / 2 - 180;
    }

    popup.style.top = `${top}px`;
    popup.style.left = `${left}px`;
  }

  function showLoadingPopup() {
    const popup = createPopup();
    positionPopupNearSelection();
    popup.innerHTML = `
      <div style="padding:24px;text-align:center;">
        <div style="font-size:28px;margin-bottom:12px;">⏳</div>
        <div style="font-weight:500;margin-bottom:4px;">正在分析文本...</div>
        <div style="font-size:12px;color:#909399;">调用 GEO API 检测内容质量</div>
      </div>
    `;
    popup.style.display = 'block';
  }

  function showErrorPopup(message) {
    const popup = createPopup();
    positionPopupNearSelection();
    popup.innerHTML = `
      <div style="padding:20px;">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">
          <div style="font-weight:600;font-size:14px;">分析失败</div>
          <button id="geo-popup-close" style="background:none;border:none;cursor:pointer;font-size:18px;color:#909399;line-height:1;">×</button>
        </div>
        <div style="background:#fef0f0;color:#f56c6c;padding:10px 12px;border-radius:6px;font-size:12px;line-height:1.5;">${escapeHtml(message)}</div>
      </div>
    `;
    popup.style.display = 'block';
    bindCloseBtn();
  }

  function showResultPopup(data, rawText) {
    const popup = createPopup();
    positionPopupNearSelection();

    const score = data.score ?? data.overallScore ?? 0;
    const scoreColor = score >= 80 ? '#52c41a' : score >= 60 ? '#faad14' : '#ff4d4f';
    const scoreDesc = score >= 80 ? '✨ 内容优秀' : score >= 60 ? '💡 可优化' : '⚠️ 需改进';
    const suggestions = data.suggestions || data.issues || [];

    let suggestionsHtml = '';
    if (suggestions.length > 0) {
      suggestionsHtml = '<div style="font-weight:600;margin:14px 0 8px;">优化建议</div>';
      suggestionsHtml += suggestions.map(s => {
        const type = s.severity || s.level || s.type || 'info';
        const dotColor = { error: '#ff4d4f', warn: '#faad14', warning: '#faad14', good: '#52c41a', success: '#52c41a' }[type] || '#1a73e8';
        const text = s.message || s.text || s.description || s.title || JSON.stringify(s);
        return `<div style="padding:8px 10px;background:#f5f7fa;border-radius:6px;margin-bottom:6px;font-size:12px;line-height:1.5;border-left:3px solid ${dotColor};">${escapeHtml(text)}</div>`;
      }).join('');
    }

    const previewText = rawText.length > 80 ? rawText.slice(0, 80) + '...' : rawText;

    popup.innerHTML = `
      <div style="padding:16px;">
        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;">
          <div style="font-weight:600;font-size:14px;">GEO 文本分析结果</div>
          <button id="geo-popup-close" style="background:none;border:none;cursor:pointer;font-size:18px;color:#909399;line-height:1;padding:0 4px;">×</button>
        </div>

        <div style="background:#f5f7fa;border-radius:6px;padding:8px 10px;font-size:11px;color:#606266;line-height:1.5;margin-bottom:14px;">
          「${escapeHtml(previewText)}」
        </div>

        <div style="text-align:center;padding:16px;background:linear-gradient(135deg,#f0f7ff,#e8f0fe);border-radius:8px;">
          <div style="font-size:12px;color:#606266;margin-bottom:4px;">GEO 质量评分</div>
          <div style="font-size:44px;font-weight:700;color:${scoreColor};line-height:1.1;">${Math.round(score)}</div>
          <div style="font-size:12px;color:#606266;margin-top:4px;">${scoreDesc}</div>
        </div>

        ${suggestionsHtml}
      </div>
    `;
    popup.style.display = 'block';
    bindCloseBtn();
  }

  function hidePopup() {
    if (popupEl) {
      popupEl.style.display = 'none';
    }
  }

  function bindCloseBtn() {
    const btn = document.getElementById('geo-popup-close');
    if (btn) {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        hidePopup();
      });
    }
  }

  function escapeHtml(str) {
    const div = document.createElement('div');
    div.textContent = String(str ?? '');
    return div.innerHTML;
  }
})();
