/**
 * GEO Content AI Optimizer - Shopify Theme Embed
 * ---------------------------------------------------------------
 * 用途：作为 Liquid snippet 嵌入到 Shopify 主题的产品页 / 文章页
 *       底部（section-footer 之前），调用自建 GEO 服务的
 *       POST /api/v1/cms/check，在页面右下方展示侧边评分卡
 *       与优化建议列表。
 *
 * 安装（Liquid snippet，例如 snippets/geo-score-card.liquid）：
 *   {% comment %} GEO 评分卡，仅在产品/文章页渲染 {% endcomment %}
 *   {% if template contains 'product' or template contains 'article' %}
 *     <div id="geo-score-card-root"
 *          data-geo-endpoint="{{ settings.geo_endpoint | default: '' }}"
 *          data-geo-token="{{ settings.geo_token | default: '' }}"
 *          data-geo-auto="{{ settings.geo_auto_run | default: 'true' }}"
 *          data-geo-primary="{{ settings.geo_primary_color | default: '#3B82F6' }}"
 *          data-geo-brand="{{ shop.name }}"></div>
 *     {{ 'theme-embed.js' | asset_url | script_tag }}
 *   {% endif %}
 *
 * 并在 config/settings_schema.json 添加：
 *   {
 *     "name": "GEO 内容优化",
 *     "settings": [
 *       { "type": "text", "id": "geo_endpoint", "label": "GEO 服务地址 (GEO_ENDPOINT)" },
 *       { "type": "text", "id": "geo_token", "label": "访问令牌 (GEO_TOKEN，可选)" },
 *       { "type": "checkbox", "id": "geo_auto_run", "label": "页面加载时自动评分", "default": true },
 *       { "type": "color", "id": "geo_primary_color", "label": "主题色", "default": "#3B82F6" }
 *     ]
 *   }
 * ---------------------------------------------------------------
 */

(function (global) {
    'use strict';

    var VERSION = '1.0.0';
    var DEFAULT_OK_THRESHOLD = 60;
    var CACHE_PREFIX = '__geo_cache__:';
    var CACHE_TTL_MS = 30 * 60 * 1000;
    var STORAGE_KEY_NS = 'geo_wp_shopify_cache_v1';

    function qs(sel, root) { return (root || document).querySelector(sel); }
    function qsa(sel, root) { return Array.prototype.slice.call((root || document).querySelectorAll(sel)); }
    function escapeHtml(s) {
        if (s == null) return '';
        return String(s)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    }
    function readStorage(key) {
        try {
            var raw = localStorage.getItem(STORAGE_KEY_NS + ':' + key);
            if (!raw) return null;
            return JSON.parse(raw);
        } catch (e) { return null; }
    }
    function writeStorage(key, val, ttlMs) {
        try {
            var payload = { t: Date.now() + (ttlMs || CACHE_TTL_MS), v: val };
            localStorage.setItem(STORAGE_KEY_NS + ':' + key, JSON.stringify(payload));
        } catch (e) {}
    }
    function readCache(key) {
        var box = readStorage(key);
        if (!box || typeof box !== 'object') return null;
        if (typeof box.t !== 'number' || Date.now() > box.t) return null;
        return box.v;
    }

    function gradeColor(grade) {
        switch (grade) {
            case 'A': return '#10B981';
            case 'B': return '#3B82F6';
            case 'C': return '#F59E0B';
            case 'D': return '#F97316';
            case 'F': return '#EF4444';
            default:  return '#6B7280';
        }
    }
    function priorityBadge(priority, primary) {
        var map = { high: '#DC2626', medium: '#F59E0B', low: '#3B82F6' };
        var label = { high: '高优先', medium: '中优先', low: '低优先' };
        var color = map[priority] || map.medium;
        return '<span style="display:inline-block;padding:2px 8px;border-radius:9999px;font-size:11px;font-weight:600;color:#fff;background:' + color + ';">' + label[priority] + '</span>';
    }

    function extractPageContent() {
        var title = '';
        var html = '';
        var url = location.href;
        var productJson = null;
        try {
            var s = qs('script[type="application/ld+json"]');
            if (s) {
                var parsed = JSON.parse(s.textContent || '{}');
                if (Array.isArray(parsed)) parsed = parsed[0] || {};
                if (parsed && parsed['@type'] === 'Product') productJson = parsed;
            }
        } catch (e) {}

        var titleEl = qs('h1.product__title, h1.product-single__title, h1.article__title, .product-title, .card__heading h1, h1');
        if (titleEl) title = titleEl.innerText.trim();
        if (!title && productJson && productJson.name) title = productJson.name;

        var bodyParts = [];
        if (qs('.product__description, .product-single__description, .rte, .article__body, .page-content, .product-description, .description')) {
            qsa('.product__description, .product-single__description, .rte, .article__body, .page-content, .product-description, .description').forEach(function (el) {
                bodyParts.push(el.innerHTML);
            });
        } else {
            var main = qs('main, [role="main"], .main-content, #MainContent') || document.body;
            if (main) bodyParts.push(main.innerHTML);
        }
        html = bodyParts.join('\n\n');
        return { title: title, html: html, url: url };
    }

    function GEOEmbed(root) {
        this.root = root;
        this.endpoint = (root.getAttribute('data-geo-endpoint') || '').replace(/\/+$/, '');
        this.token = root.getAttribute('data-geo-token') || '';
        this.autoRun = (root.getAttribute('data-geo-auto') || 'true') === 'true';
        this.primary = root.getAttribute('data-geo-primary') || '#3B82F6';
        this.brand = root.getAttribute('data-geo-brand') || 'GEO';
        this.state = { loading: false, result: null, error: null };
        this._buildUI();
        if (this.autoRun) {
            this.scheduleCheck();
        }
    }

    GEOEmbed.prototype._buildUI = function () {
        var host = document.createElement('div');
        host.id = 'geo-score-card-widget';
        host.setAttribute('data-version', VERSION);
        host.innerHTML = [
            '<style>',
            '#geo-score-card-widget{--geo-primary:' + this.primary + ';position:fixed;right:20px;bottom:20px;z-index:2147483000;font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif;font-size:14px;color:#111827;}',
            '#geo-score-card-widget .geo-card{width:340px;max-height:80vh;background:#fff;border-radius:14px;box-shadow:0 10px 30px rgba(17,24,39,0.12);border:1px solid #E5E7EB;overflow:hidden;display:flex;flex-direction:column;transition:max-height 200ms ease, opacity 200ms ease;}',
            '#geo-score-card-widget .geo-card.collapsed{max-height:52px;}',
            '#geo-score-card-widget .geo-header{display:flex;align-items:center;justify-content:space-between;padding:10px 14px;background:linear-gradient(135deg,var(--geo-primary),var(--geo-primary));color:#fff;cursor:pointer;user-select:none;}',
            '#geo-score-card-widget .geo-header-title{display:flex;align-items:center;gap:8px;font-weight:600;font-size:13px;}',
            '#geo-score-card-widget .geo-toggle{width:22px;height:22px;border-radius:6px;background:rgba(255,255,255,0.18);display:flex;align-items:center;justify-content:center;transition:transform 200ms ease;}',
            '#geo-score-card-widget .geo-card.collapsed .geo-toggle{transform:rotate(180deg);}',
            '#geo-score-card-widget .geo-body{padding:14px;overflow-y:auto;}',
            '#geo-score-card-widget .geo-score-row{display:flex;align-items:center;gap:12px;margin-bottom:12px;}',
            '#geo-score-card-widget .geo-grade{width:48px;height:48px;border-radius:12px;display:flex;align-items:center;justify-content:center;color:#fff;font-weight:800;font-size:22px;box-shadow:0 4px 12px rgba(17,24,39,0.12);}',
            '#geo-score-card-widget .geo-score-main{flex:1;}',
            '#geo-score-card-widget .geo-score-val{font-size:24px;font-weight:700;line-height:1.2;}',
            '#geo-score-card-widget .geo-ok{display:inline-block;margin-top:4px;padding:2px 8px;border-radius:9999px;font-size:12px;font-weight:600;}',
            '#geo-score-card-widget .geo-ok.ok{background:#D1FAE5;color:#065F46;}',
            '#geo-score-card-widget .geo-ok.no{background:#FEE2E2;color:#991B1B;}',
            '#geo-score-card-widget .geo-progress{height:8px;border-radius:9999px;background:#E5E7EB;overflow:hidden;margin:12px 0 16px;}',
            '#geo-score-card-widget .geo-progress-bar{height:100%;border-radius:9999px;background:var(--geo-primary);transition:width 400ms ease;width:0%;}',
            '#geo-score-card-widget .geo-section-title{font-size:12px;text-transform:uppercase;letter-spacing:0.5px;color:#6B7280;font-weight:600;margin:0 0 8px;}',
            '#geo-score-card-widget .geo-suggestions{list-style:none;margin:0;padding:0;display:flex;flex-direction:column;gap:8px;}',
            '#geo-score-card-widget .geo-suggestion{display:flex;gap:8px;padding:10px;border-radius:10px;background:#F9FAFB;border:1px solid #F3F4F6;}',
            '#geo-score-card-widget .geo-sug-text{flex:1;font-size:13px;line-height:1.5;color:#111827;}',
            '#geo-score-card-widget .geo-sug-cat{font-size:11px;font-weight:600;color:#6B7280;text-transform:uppercase;margin-top:4px;}',
            '#geo-score-card-widget .geo-signals{display:grid;grid-template-columns:repeat(2,1fr);gap:6px;margin-top:6px;}',
            '#geo-score-card-widget .geo-signal{display:flex;align-items:center;gap:6px;padding:6px 8px;border-radius:8px;background:#F9FAFB;font-size:12px;}',
            '#geo-score-card-widget .geo-dot{width:8px;height:8px;border-radius:9999px;background:#D1D5DB;flex-shrink:0;}',
            '#geo-score-card-widget .geo-dot.yes{background:#10B981;}',
            '#geo-score-card-widget .geo-actions{display:flex;gap:8px;margin-top:12px;}',
            '#geo-score-card-widget button.geo-btn{flex:1;border:none;border-radius:8px;padding:8px 10px;font-size:12px;font-weight:600;cursor:pointer;transition:opacity 120ms ease;background:var(--geo-primary);color:#fff;}',
            '#geo-score-card-widget button.geo-btn:hover{opacity:0.9;}',
            '#geo-score-card-widget button.geo-btn.secondary{background:#F3F4F6;color:#111827;}',
            '#geo-score-card-widget .geo-error{padding:12px;border-radius:10px;background:#FEF2F2;border:1px solid #FECACA;color:#991B1B;font-size:13px;}',
            '#geo-score-card-widget .geo-loader{display:inline-block;width:14px;height:14px;border:2px solid #fff;border-top-color:transparent;border-radius:50%;animation:geo-spin 0.8s linear infinite;}',
            '@keyframes geo-spin{to{transform:rotate(360deg);}}',
            '#geo-score-card-widget .geo-footer{padding:10px 14px;border-top:1px solid #F3F4F6;font-size:11px;color:#6B7280;display:flex;justify-content:space-between;align-items:center;}',
            '#geo-score-card-widget .geo-footer a{color:var(--geo-primary);text-decoration:none;}',
            '</style>',
            '<div class="geo-card collapsed" id="geo-card">',
            '  <div class="geo-header" id="geo-header">',
            '    <div class="geo-header-title" id="geo-header-title">',
            '      <span class="geo-loader" style="display:none;" id="geo-loader"></span>',
            '      <span id="geo-title-text">' + escapeHtml(this.brand) + ' GEO 评分</span>',
            '    </div>',
            '    <div class="geo-toggle">',
            '      <svg width="14" height="14" viewBox="0 0 20 20" fill="currentColor" aria-hidden="true"><path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.06l3.71-3.83a.75.75 0 111.08 1.04l-4.24 4.38a.75.75 0 01-1.08 0L5.21 8.27a.75.75 0 01.02-1.06z" clip-rule="evenodd"/></svg>',
            '    </div>',
            '  </div>',
            '  <div class="geo-body" id="geo-body">',
            '    <div id="geo-content">',
            '      <div style="color:#6B7280;font-size:13px;">' + escapeHtml(this.brand) + ' GEO 内容可见度评分卡片。点击右上角重新分析。</div>',
            '    </div>',
            '    <div class="geo-actions">',
            '      <button class="geo-btn" id="geo-btn-check">立即分析</button>',
            '      <button class="geo-btn secondary" id="geo-btn-copy">复制建议</button>',
            '    </div>',
            '  </div>',
            '  <div class="geo-footer">',
            '    <span>Powered by <strong>GEO</strong> v' + VERSION + '</span>',
            '    <span><a href="javascript:void(0);" id="geo-info">服务信息</a></span>',
            '  </div>',
            '</div>'
        ].join('\n');
        this.root.innerHTML = '';
        this.root.appendChild(host);

        this._card = qs('#geo-card', host);
        this._content = qs('#geo-content', host);
        this._loader = qs('#geo-loader', host);
        this._titleText = qs('#geo-title-text', host);
        var self = this;
        qs('#geo-header', host).addEventListener('click', function () {
            self._card.classList.toggle('collapsed');
        });
        qs('#geo-btn-check', host).addEventListener('click', function () {
            self.check(true);
        });
        qs('#geo-btn-copy', host).addEventListener('click', function () {
            self.copySuggestions();
        });
        qs('#geo-info', host).addEventListener('click', function () {
            self.showInfo();
        });
        setTimeout(function () { self._card.classList.remove('collapsed'); }, 600);
    };

    GEOEmbed.prototype._setLoading = function (loading) {
        this.state.loading = !!loading;
        this._loader.style.display = loading ? 'inline-block' : 'none';
        this._titleText.textContent = loading ? '分析中…' : (this.brand + ' GEO 评分');
    };

    GEOEmbed.prototype.scheduleCheck = function () {
        var self = this;
        var run = function () {
            try {
                var info = extractPageContent();
                var key = info.url || location.pathname;
                var cached = readCache(CACHE_PREFIX + key);
                if (cached) {
                    self.state.result = cached;
                    self._renderResult();
                    self._card.classList.add('collapsed');
                    return;
                }
            } catch (e) {}
            self.check(false);
        };
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', run);
        } else {
            setTimeout(run, 200);
        }
    };

    GEOEmbed.prototype.check = function (force) {
        var self = this;
        if (!this.endpoint) {
            this._renderError('GEO_ENDPOINT 未配置。请在 Shopify 主题设置中填写自建 GEO 服务地址。');
            return;
        }
        this._setLoading(true);
        this.state.error = null;
        var info = extractPageContent();
        var body = {
            html: info.html || '',
            url: info.url || location.href,
            title: info.title || document.title,
            domain: location.hostname
        };
        var cacheKey = CACHE_PREFIX + (body.url || location.pathname);
        var headers = { 'Content-Type': 'application/json', 'Accept': 'application/json' };
        if (this.token) headers['Authorization'] = 'Bearer ' + this.token;
        fetch(this.endpoint + '/api/v1/cms/check', {
            method: 'POST',
            headers: headers,
            body: JSON.stringify(body)
        }).then(function (r) {
            return r.json().then(function (d) { return { ok: r.ok, status: r.status, data: d }; });
        }).then(function (res) {
            self._setLoading(false);
            if (!res.ok) {
                var msg = (res.data && res.data.error) ? res.data.error : ('HTTP ' + res.status);
                self._renderError('GEO 服务请求失败：' + msg);
                return;
            }
            self.state.result = res.data;
            writeStorage(cacheKey, res.data, CACHE_TTL_MS);
            self._renderResult();
            self._card.classList.remove('collapsed');
        }).catch(function (e) {
            self._setLoading(false);
            self._renderError('网络错误：' + (e.message || String(e)));
        });
    };

    GEOEmbed.prototype._renderError = function (msg) {
        this.state.error = msg;
        this._content.innerHTML = '<div class="geo-error">' + escapeHtml(msg) + '</div>';
    };

    GEOEmbed.prototype._renderResult = function () {
        var r = this.state.result;
        if (!r) return;
        var score = typeof r.score === 'number' ? r.score : 0;
        var grade = r.grade || 'F';
        var ok = !!r.ok;
        var suggestions = Array.isArray(r.suggestions) ? r.suggestions : [];
        var signals = (r.signals && typeof r.signals === 'object') ? r.signals : {};
        var citability = signals.citability_signals || {};
        var structure = signals.structure_signals || {};
        var negatives = Array.isArray(signals.negative_signals) ? signals.negative_signals : [];
        var evg = typeof signals.evergreen_score === 'number' ? signals.evergreen_score : 0;
        var wc = typeof signals.word_count === 'number' ? signals.word_count : 0;

        var okLabel = ok ? '✓ GEO 通过' : '⚠ 待优化';
        var okClass = ok ? 'ok' : 'no';
        var gc = gradeColor(grade);

        var sigHtml = [];
        Object.keys(citability).forEach(function (k) {
            sigHtml.push('<div class="geo-signal"><span class="geo-dot ' + (citability[k] ? 'yes' : '') + '"></span><span>' + (citability[k] ? '✓ ' : '✗ ') + escapeHtml(k) + '</span></div>');
        });
        Object.keys(structure).forEach(function (k) {
            sigHtml.push('<div class="geo-signal"><span class="geo-dot ' + (structure[k] ? 'yes' : '') + '"></span><span>' + (structure[k] ? '✓ ' : '✗ ') + escapeHtml(k) + '</span></div>');
        });
        var sigBlock = '<h4 class="geo-section-title">信号检测 · ' + escapeHtml(String(wc)) + ' 词 · 常青度 ' + escapeHtml(String(evg)) + '</h4>' +
            '<div class="geo-signals">' + sigHtml.join('') + '</div>';
        if (negatives.length > 0) {
            sigBlock += '<div style="margin-top:8px;padding:8px 10px;border-radius:8px;background:#FEF2F2;color:#991B1B;font-size:12px;">⚠ 负向信号：' + escapeHtml(negatives.join(', ')) + '</div>';
        }

        var sugHtml = [];
        if (suggestions.length === 0) {
            sugHtml.push('<div style="padding:10px;border-radius:10px;background:#ECFDF5;color:#065F46;font-size:13px;">🎉 暂无明显优化建议，内容质量良好！</div>');
        } else {
            suggestions.forEach(function (s) {
                sugHtml.push(
                    '<li class="geo-suggestion">' +
                    priorityBadge(s.priority, this.primary) +
                    '<div class="geo-sug-text">' +
                    escapeHtml(s.message) +
                    '<div class="geo-sug-cat">' + escapeHtml(s.category || 'general') + '</div>' +
                    '</div></li>'
                );
            });
        }

        this._content.innerHTML = [
            '<div class="geo-score-row">',
            '  <div class="geo-grade" style="background:' + gc + ';">' + escapeHtml(grade) + '</div>',
            '  <div class="geo-score-main">',
            '    <div class="geo-score-val">' + score.toFixed(1) + ' <span style="font-size:14px;color:#6B7280;font-weight:400;">/ 100</span></div>',
            '    <span class="geo-ok ' + okClass + '">' + okLabel + '</span>',
            '  </div>',
            '</div>',
            '<div class="geo-progress"><div class="geo-progress-bar" style="width:' + Math.min(100, Math.max(0, score)) + '%;background:' + gc + ';"></div></div>',
            '<h4 class="geo-section-title">优化建议 (' + suggestions.length + ')</h4>',
            '<ul class="geo-suggestions">' + sugHtml.join('') + '</ul>',
            '<div style="margin-top:16px;">' + sigBlock + '</div>'
        ].join('\n');
    };

    GEOEmbed.prototype.copySuggestions = function () {
        var r = this.state.result;
        var text = '';
        if (!r) {
            text = '尚未执行 GEO 评分，请先点击"立即分析"。';
        } else {
            text = 'GEO 评分：' + (typeof r.score === 'number' ? r.score.toFixed(1) : '0.0') + ' (' + (r.grade || 'F') + ')\n';
            text += 'URL：' + location.href + '\n\n';
            text += '优化建议：\n';
            (r.suggestions || []).forEach(function (s, i) {
                text += (i + 1) + '. [' + (s.priority || 'medium') + '] ' + (s.category || '') + ' — ' + s.message + '\n';
            });
        }
        try {
            var ta = document.createElement('textarea');
            ta.value = text;
            ta.style.position = 'fixed';
            ta.style.left = '-9999px';
            document.body.appendChild(ta);
            ta.select();
            document.execCommand('copy');
            document.body.removeChild(ta);
            this._toast('已复制优化建议到剪贴板');
        } catch (e) {
            window.prompt('复制以下建议：', text);
        }
    };

    GEOEmbed.prototype._toast = function (msg) {
        var t = document.createElement('div');
        t.textContent = msg;
        t.style.cssText = 'position:fixed;left:50%;bottom:100px;transform:translateX(-50%);background:rgba(17,24,39,0.9);color:#fff;padding:8px 14px;border-radius:8px;font-size:13px;z-index:2147483600;transition:opacity 200ms ease;';
        document.body.appendChild(t);
        setTimeout(function () { t.style.opacity = '0'; }, 1500);
        setTimeout(function () { if (t.parentNode) t.parentNode.removeChild(t); }, 2000);
    };

    GEOEmbed.prototype.showInfo = function () {
        var self = this;
        if (!this.endpoint) {
            this._toast('GEO_ENDPOINT 未配置');
            return;
        }
        var headers = { 'Accept': 'application/json' };
        if (this.token) headers['Authorization'] = 'Bearer ' + this.token;
        fetch(this.endpoint + '/api/v1/cms/info', { headers: headers })
            .then(function (r) { return r.json(); })
            .then(function (d) {
                var wl = (d && d.whitelabel) || {};
                alert([
                    '品牌：' + (wl.brand_name || self.brand),
                    'GEO 服务版本：' + (d && d.version ? d.version : '未知'),
                    '白标域名：' + (wl.domain || '-'),
                    'Check 端点：' + (d && d.endpoints && d.endpoints.check ? d.endpoints.check : '/api/v1/cms/check'),
                    '嵌入 SDK 版本：' + VERSION
                ].join('\n'));
            })
            .catch(function (e) { self._toast('获取服务信息失败：' + (e.message || String(e))); });
    };

    function autoInit() {
        var root = qs('#geo-score-card-root');
        if (!root) return;
        if (root._geoEmbedInitialized) return;
        root._geoEmbedInitialized = true;
        try {
            new GEOEmbed(root);
        } catch (e) {
            console.error('[GEO Embed] 初始化失败：', e);
        }
    }

    if (typeof module !== 'undefined' && module.exports) {
        module.exports = { GEOEmbed: GEOEmbed, extractPageContent: extractPageContent };
    } else {
        global.GEOEmbed = GEOEmbed;
        global.GEOEmbedExtract = extractPageContent;
        if (document.readyState === 'loading') {
            document.addEventListener('DOMContentLoaded', autoInit);
        } else {
            autoInit();
        }
    }

})(typeof window !== 'undefined' ? window : this);
