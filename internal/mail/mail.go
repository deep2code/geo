// Package mail 提供 SMTP 邮件发送能力，用于告警、周报、PDF 报告投递。
//
// 纯标准库实现（net/smtp + html/template + mime 邮件组装），零外部依赖。
// 支持：
//   - HTML + Plain Text 双版本邮件（multipart/alternative）
//   - 附件（multipart/mixed，PDF/JSON 等）
//   - Sender 复用 SMTP 连接（批量场景复用）
//   - 环境变量一键配置（GEO_SMTP_*）
//
// 环境变量：
//
//	GEO_SMTP_HOST       SMTP 服务器主机（如 smtp.qq.com）
//	GEO_SMTP_PORT       SMTP 端口（默认 587，TLS/STARTTLS）
//	GEO_SMTP_USER       SMTP 用户名
//	GEO_SMTP_PASSWORD   SMTP 密码/授权码
//	GEO_SMTP_FROM       发件人显示（含 <email>），例如 "GEO 告警 <no-reply@geo.io>"
//	GEO_SMTP_TLS        auto/starttls/ssl/none（默认 auto：25=none 465=ssl 其它=starttls）
package mail

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"my-geo/internal/config"
	"my-geo/internal/util"
)

// aiMailDisclaimerShort 邮件末尾附的短版 AI 生成声明（法务 #81）。
// 放在所有邮件（告警、周报、报告）的 HTML footer；纯文本同步附短声明。
const aiMailDisclaimerShort = "内容由 AI 生成，仅供参考，不构成商业/法律建议。"

const aiMailFooterHTML = `
<div style="border-top:1px solid #f1f5f9;margin-top:20px;padding-top:12px;color:#64748b;font-size:12px;line-height:1.6">
  <div style="color:#b45309">⚠️ ` + aiMailDisclaimerShort + `</div>
  <div>合规与投诉：<a href="mailto:` + util.MyGEOComplianceEmail + `" style="color:#2563eb;text-decoration:none">` + util.MyGEOComplianceEmail + `</a>
  · <a href="/legal/bot" style="color:#2563eb;text-decoration:none">爬虫声明</a>
  · <a href="/privacy" style="color:#2563eb;text-decoration:none">隐私政策</a>
  · <a href="/terms" style="color:#2563eb;text-decoration:none">服务条款</a></div>
</div>`

const aiMailFooterText = "\n\n-- \n" + aiMailDisclaimerShort +
	"\n合规与投诉：" + util.MyGEOComplianceEmail +
	"\n爬虫声明：/legal/bot · 隐私政策：/privacy · 服务条款：/terms"

// Attachment 邮件附件。
type Attachment struct {
	Filename    string // 文件名
	ContentType string // MIME 类型，空时从文件名推断
	Content     []byte // 附件二进制内容
	Source      string // 可选：文件路径（Content 为空时从文件读）
}

// Message 一封待发送邮件。
type Message struct {
	To          []string     // 收件人
	Cc          []string     // 抄送
	Bcc         []string     // 密送
	Subject     string       // 主题
	TextBody    string       // 纯文本正文（可选）
	HTMLBody    string       // HTML 正文（可选，TextBody 与 HTMLBody 至少一个）
	Attachments []Attachment // 附件（可选）
}

// Sender SMTP 邮件发送器。
type Sender struct {
	Host     string
	Port     int
	User     string
	Password string
	From     string // From 头，例如 "GEO 系统 <no-reply@geo.io>"
	FromAddr string // 仅邮件地址部分（SMTP MAIL FROM）
	TLS      string // auto/starttls/ssl/none
}

// NewSender 从环境变量构建 Sender。
//
// 未配置主机时返回 (nil, nil) 表示禁用邮件功能（调用方可以降级为日志打印）。
func NewSender() (*Sender, error) {
	host := config.Env("GEO_SMTP_HOST", "")
	if host == "" {
		return nil, nil
	}
	portStr := config.Env("GEO_SMTP_PORT", "587")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("mail: 无效 GEO_SMTP_PORT %q: %w", portStr, err)
	}
	user := config.Env("GEO_SMTP_USER", "")
	pass := config.Env("GEO_SMTP_PASSWORD", "")
	from := config.Env("GEO_SMTP_FROM", user)
	tlsMode := config.Env("GEO_SMTP_TLS", "auto")
	return NewSenderFromConfig(host, port, user, pass, from, tlsMode)
}

// NewSenderFromConfig 手动配置构建 Sender。
func NewSenderFromConfig(host string, port int, user, password, from, tlsMode string) (*Sender, error) {
	fromAddr := extractEmail(from)
	if fromAddr == "" {
		fromAddr = user // 回退到用户名
	}
	// P1-9：发件人显示名/地址防头注入（配置来自环境变量，防御 CR/LF 注入）。
	// 注意：不强制要求 @（GEO_SMTP_FROM 未配置时回退 user 可能为空），
	// 仅拒绝注入向量，避免破坏未配置发件人的既有部署。
	if strings.ContainsAny(from, "\r\n\x00") || strings.ContainsAny(fromAddr, "\r\n\x00") {
		return nil, fmt.Errorf("mail: 发件人配置包含非法控制字符（CR/LF）")
	}
	return &Sender{
		Host: host, Port: port,
		User: user, Password: password,
		From: from, FromAddr: fromAddr,
		TLS: resolveTLSMode(tlsMode, port),
	}, nil
}

// Enabled 判断 Sender 是否配置可用（不保证可达）。
func (s *Sender) Enabled() bool { return s != nil && s.Host != "" }

// Send 发送一封邮件。
func (s *Sender) Send(m *Message) error {
	if !s.Enabled() {
		return fmt.Errorf("mail: SMTP 未配置")
	}
	// P1-9：头注入防护——所有收件人在入口统一校验（拒绝 CR/LF/控制字符/分隔符）。
	// To/Cc/Bcc 会拼接进邮件头，未校验的地址可注入额外 Bcc 头或伪造收件人。
	for _, group := range [][]string{m.To, m.Cc, m.Bcc} {
		for _, addr := range group {
			if err := validateRecipient(addr); err != nil {
				return err
			}
		}
	}
	recipients := dedupEmails(append(append(m.To, m.Cc...), m.Bcc...))
	if len(recipients) == 0 {
		return fmt.Errorf("mail: 没有收件人")
	}
	// 法务 #81：所有 AI 生成邮件统一追加免责声明与合规链接（HTML/Plain 双版本）。
	// 保证每个投递的邮件都有合规声明，调用方无需关心插入逻辑。
	withLegal(m)
	data, err := s.compose(m)
	if err != nil {
		return err
	}
	return s.deliver(recipients, data)
}

// withLegal 给 Message 注入 AI 生成声明与合规页脚（法务 #81）。
func withLegal(m *Message) {
	if m == nil {
		return
	}
	if m.TextBody != "" && !strings.Contains(m.TextBody, aiMailDisclaimerShort) {
		m.TextBody = m.TextBody + aiMailFooterText
	}
	if m.HTMLBody != "" && !strings.Contains(m.HTMLBody, aiMailDisclaimerShort) {
		// 优先插入到 </body> 前；无 body 标记时追加在末尾
		lower := strings.ToLower(m.HTMLBody)
		idx := strings.LastIndex(lower, "</body>")
		if idx >= 0 {
			m.HTMLBody = m.HTMLBody[:idx] + aiMailFooterHTML + m.HTMLBody[idx:]
		} else {
			m.HTMLBody = m.HTMLBody + aiMailFooterHTML
		}
	}
}

// Deliver 低阶：直接投递已组装的邮件字节。
func (s *Sender) deliver(recipients []string, data []byte) error {
	addr := s.Host + ":" + strconv.Itoa(s.Port)

	// SSL 直接 TLS（465 端口常见）
	if s.TLS == "ssl" {
		return s.deliverSSL(addr, recipients, data)
	}

	// 普通连接 + 可选 STARTTLS（25/587）
	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return fmt.Errorf("mail: 连接 SMTP 失败: %w", err)
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		return fmt.Errorf("mail: SMTP 握手失败: %w", err)
	}
	defer c.Quit()

	// P1-10：STARTTLS 降级明文防护。
	//   starttls 模式：强制加密，服务器不支持时直接报错（绝不明文发凭据）；
	//   auto 模式：支持则加密；不支持时若有凭据则报错（防止明文传输 SMTP 密码），
	//   无凭据（匿名中继）才允许降级明文并记警告。
	if s.TLS == "starttls" || s.TLS == "auto" {
		ok, _ := c.Extension("STARTTLS")
		switch {
		case ok:
			if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
				return fmt.Errorf("mail: STARTTLS 失败: %w", err)
			}
		case s.TLS == "starttls":
			return fmt.Errorf("mail: 服务器 %s 不支持 STARTTLS（GEO_SMTP_TLS=starttls 强制加密）", s.Host)
		case s.User != "":
			return fmt.Errorf("mail: 服务器 %s 不支持 STARTTLS，且配置了 SMTP 凭据，拒绝明文认证（可显式设置 GEO_SMTP_TLS=none 降级）", s.Host)
		default:
			slog.Warn("mail: SMTP 服务器不支持 STARTTLS，匿名发送降级为明文", "host", s.Host, "port", s.Port)
		}
	}
	if s.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
			return fmt.Errorf("mail: SMTP 认证失败: %w", err)
		}
	}
	if err := c.Mail(s.FromAddr); err != nil {
		return fmt.Errorf("mail: MAIL FROM 失败: %w", err)
	}
	for _, rcpt := range recipients {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("mail: RCPT TO %s 失败: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA 失败: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("mail: 写入邮件内容失败: %w", err)
	}
	return w.Close()
}

// deliverSSL 使用直接 TLS（465）。
func (s *Sender) deliverSSL(addr string, recipients []string, data []byte) error {
	tlsc, err := tls.Dial("tcp", addr, &tls.Config{ServerName: s.Host})
	if err != nil {
		return fmt.Errorf("mail: SSL 连接失败: %w", err)
	}
	defer tlsc.Close()
	c, err := smtp.NewClient(tlsc, s.Host)
	if err != nil {
		return fmt.Errorf("mail: SSL SMTP 握手失败: %w", err)
	}
	defer c.Quit()
	if s.User != "" {
		if err := c.Auth(smtp.PlainAuth("", s.User, s.Password, s.Host)); err != nil {
			return fmt.Errorf("mail: SMTP 认证失败: %w", err)
		}
	}
	if err := c.Mail(s.FromAddr); err != nil {
		return fmt.Errorf("mail: MAIL FROM 失败: %w", err)
	}
	for _, rcpt := range recipients {
		if err := c.Rcpt(rcpt); err != nil {
			return fmt.Errorf("mail: RCPT TO %s 失败: %w", rcpt, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA 失败: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("mail: 写入邮件内容失败: %w", err)
	}
	return w.Close()
}

// compose 组装邮件 MIME 内容。
func (s *Sender) compose(m *Message) ([]byte, error) {
	var b bytes.Buffer
	headers := textproto.MIMEHeader{}
	headers.Set("From", s.From)
	headers.Set("To", strings.Join(m.To, ", "))
	if len(m.Cc) > 0 {
		headers.Set("Cc", strings.Join(m.Cc, ", "))
	}
	headers.Set("Subject", mime.QEncoding.Encode("UTF-8", m.Subject))
	headers.Set("Date", time.Now().Format(time.RFC1123Z))
	headers.Set("MIME-Version", "1.0")
	headers.Set("Message-ID", fmt.Sprintf("<%d.geo@%s>", time.Now().UnixNano(), hostname()))

	hasAttach := len(m.Attachments) > 0
	multipart := "multipart/alternative"
	if hasAttach {
		multipart = "multipart/mixed"
	}
	boundary := genBoundary()
	headers.Set("Content-Type", fmt.Sprintf("%s; boundary=\"%s\"", multipart, boundary))

	// 写 headers
	for k, vs := range headers {
		for _, v := range vs {
			b.WriteString(k + ": " + v + "\r\n")
		}
	}
	b.WriteString("\r\n")

	// 内容区
	if hasAttach {
		// mixed 外层: 第一部分是 alternative 正文，后续是附件
		writeBoundary(&b, boundary, false)
		altBoundary := genBoundary()
		b.WriteString("Content-Type: multipart/alternative; boundary=\"" + altBoundary + "\"\r\n\r\n")
		writeBodies(&b, altBoundary, m.TextBody, m.HTMLBody)
		// 附件
		for _, att := range m.Attachments {
			writeBoundary(&b, boundary, false)
			if err := writeAttachment(&b, att); err != nil {
				return nil, err
			}
		}
		writeBoundary(&b, boundary, true)
	} else {
		writeBodies(&b, boundary, m.TextBody, m.HTMLBody)
		writeBoundary(&b, boundary, true)
	}
	return b.Bytes(), nil
}

// writeBodies 写 alternative 双版本正文。
func writeBodies(b *bytes.Buffer, boundary, text, html string) {
	if text != "" {
		writeBoundary(b, boundary, false)
		b.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		w := quotedprintable.NewWriter(b)
		io.WriteString(w, text)
		w.Close()
		b.WriteString("\r\n")
	}
	if html != "" {
		writeBoundary(b, boundary, false)
		b.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		w := quotedprintable.NewWriter(b)
		io.WriteString(w, html)
		w.Close()
		b.WriteString("\r\n")
	}
}

// writeAttachment 写单个附件（base64）。
func writeAttachment(b *bytes.Buffer, att Attachment) error {
	var content []byte
	if len(att.Content) > 0 {
		content = att.Content
	} else if att.Source != "" {
		// P1-9：附件 Source 路径白名单——只允许安全相对路径。
		// /api/v1/mail/send 接口直接接收用户传入的 Attachment.Source，
		// 若不限制，攻击者可读取服务器任意文件（如 /etc/passwd）作为附件发出。
		if !filepath.IsLocal(att.Source) {
			return fmt.Errorf("mail: 附件 Source 必须是安全相对路径（拒绝绝对路径/路径穿越）: %q", att.Source)
		}
		data, err := os.ReadFile(att.Source)
		if err != nil {
			return fmt.Errorf("mail: 读取附件文件 %s 失败: %w", att.Source, err)
		}
		content = data
	} else {
		return fmt.Errorf("mail: 附件 %s 无内容", att.Filename)
	}
	ct := att.ContentType
	if ct == "" {
		ct = mime.TypeByExtension(filepath.Ext(att.Filename))
		if ct == "" {
			ct = "application/octet-stream"
		}
	}
	name := mime.QEncoding.Encode("UTF-8", att.Filename)
	b.WriteString(fmt.Sprintf("Content-Type: %s; name=\"%s\"\r\n", ct, name))
	b.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", name))
	b.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	enc := base64.NewEncoder(base64.StdEncoding, b)
	enc.Write(content)
	enc.Close()
	b.WriteString("\r\n")
	return nil
}

func writeBoundary(b *bytes.Buffer, boundary string, last bool) {
	b.WriteString("--")
	b.WriteString(boundary)
	if last {
		b.WriteString("--")
	}
	b.WriteString("\r\n")
}

func genBoundary() string {
	return fmt.Sprintf("----=_GEO_%d_%x", time.Now().UnixNano(), randUint64())
}

func randUint64() uint64 {
	const golden = uint64(0x9e3779b97f4a7c15)
	return uint64(time.Now().UnixNano()) ^ golden
}

func hostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "geo.local"
}

// extractEmail 从 "Name <email@x.com>" 中提取 email。
func extractEmail(s string) string {
	if i := strings.LastIndex(s, "<"); i >= 0 {
		if j := strings.Index(s[i:], ">"); j > 0 {
			return strings.TrimSpace(s[i+1 : i+j])
		}
	}
	return strings.TrimSpace(s)
}

// validateRecipient 校验单个收件人地址，防止邮件头注入（P1-9）。
// 地址会拼接进 To/Cc/Bcc 头并用于 SMTP RCPT 命令，未校验的地址可
// 通过 CRLF 注入额外邮件头（如 Bcc 窃取收件人）或伪造命令。
func validateRecipient(addr string) error {
	a := strings.TrimSpace(addr)
	if a == "" {
		return fmt.Errorf("mail: 收件人地址为空")
	}
	// 头注入向量：CR / LF / NUL 等控制字符一律拒绝
	if strings.ContainsAny(a, "\r\n\x00") {
		return fmt.Errorf("mail: 收件人地址包含非法控制字符: %q", a)
	}
	// 分隔符：逗号会破坏 Cc 头结构，分号可被部分客户端解析为额外地址
	if strings.ContainsAny(a, ",;") {
		return fmt.Errorf("mail: 收件人地址包含非法分隔符: %q", a)
	}
	// 基础格式：必须含 @（至少一个），且不包含空白（地址不可含空格）
	if !strings.Contains(a, "@") {
		return fmt.Errorf("mail: 收件人地址缺少 @: %q", a)
	}
	if strings.ContainsAny(a, " \t") {
		return fmt.Errorf("mail: 收件人地址包含空白字符: %q", a)
	}
	return nil
}

func dedupEmails(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, e := range in {
		e = strings.TrimSpace(strings.ToLower(e))
		if e == "" || seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

func resolveTLSMode(mode string, port int) string {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "auto" || mode == "" {
		switch port {
		case 465:
			return "ssl"
		default:
			// 25/587 等端口默认优先 STARTTLS（多数现代服务器支持）。
			// 服务器不支持时由 deliver 按凭据有无决定降级或报错；
			// 确需明文可显式配置 GEO_SMTP_TLS=none。
			return "starttls"
		}
	}
	return mode
}

// ===== 预设模板 =====

const (
	// TemplateAlert 告警邮件 HTML 模板。
	TemplateAlert = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>{{.Subject}}</title></head>
<body style="font-family:-apple-system,PingFang SC,Microsoft YaHei,Arial,sans-serif;background:#f5f6f8;margin:0;padding:24px;color:#1f2937;">
<table width="100%" cellpadding="0" cellspacing="0" style="max-width:640px;margin:0 auto;">
<tr><td style="padding:24px 28px;background:{{if eq .Severity "critical"}}#fee2e2{{else if eq .Severity "warning"}}#fef3c7{{else}}#dbeafe{{end}};border-radius:8px 8px 0 0;">
  <div style="font-size:18px;font-weight:600;">GEO 告警 · {{.BrandName}}</div>
  <div style="margin-top:4px;font-size:13px;opacity:.8;">{{.Timestamp}}</div>
</td></tr>
<tr><td style="padding:24px 28px;background:#fff;">
  <div style="font-size:15px;line-height:1.8;">{{.Message}}</div>
  {{if .Extra}}
  <div style="margin-top:16px;padding:14px 16px;background:#f9fafb;border-radius:6px;font-size:13px;color:#374151;font-family:monospace;white-space:pre-wrap;">{{.Extra}}</div>
  {{end}}
</td></tr>
<tr><td style="padding:14px 28px;background:#f9fafb;border-radius:0 0 8px 8px;font-size:12px;color:#6b7280;">
  由 my-geo 自动发送 · 前往控制台：<a href="{{.ConsoleURL}}" style="color:#2563eb;">{{.ConsoleURL}}</a>
</td></tr></table></body></html>`

	// TemplateWeekly 周报邮件 HTML 模板（含品牌趋势表）。
	TemplateWeekly = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>{{.Subject}}</title></head>
<body style="font-family:-apple-system,PingFang SC,Microsoft YaHei,Arial,sans-serif;background:#f5f6f8;margin:0;padding:24px;color:#1f2937;">
<table width="100%" cellpadding="0" cellspacing="0" style="max-width:720px;margin:0 auto;">
<tr><td style="padding:24px 28px;background:linear-gradient(135deg,#4f46e5,#2563eb);color:#fff;border-radius:8px 8px 0 0;">
  <div style="font-size:20px;font-weight:600;">GEO 周报 · {{.WeekRange}}</div>
  <div style="margin-top:4px;font-size:13px;opacity:.85;">{{.Summary}}</div>
</td></tr>
<tr><td style="padding:24px 28px;background:#fff;">
  <table width="100%" cellpadding="0" cellspacing="0" style="border-collapse:collapse;">
  <thead><tr style="background:#f3f4f6;">
    <th style="padding:10px 12px;text-align:left;font-size:13px;border-bottom:1px solid #e5e7eb;">品牌</th>
    <th style="padding:10px 12px;text-align:right;font-size:13px;border-bottom:1px solid #e5e7eb;">BVS</th>
    <th style="padding:10px 12px;text-align:right;font-size:13px;border-bottom:1px solid #e5e7eb;">周变化</th>
    <th style="padding:10px 12px;text-align:left;font-size:13px;border-bottom:1px solid #e5e7eb;">等级</th>
  </tr></thead><tbody>
  {{range .Brands}}<tr>
    <td style="padding:10px 12px;border-bottom:1px solid #f3f4f6;">{{.Name}}</td>
    <td style="padding:10px 12px;text-align:right;border-bottom:1px solid #f3f4f6;font-weight:600;">{{.Score}}</td>
    <td style="padding:10px 12px;text-align:right;border-bottom:1px solid #f3f4f6;color:{{if lt .Delta 0.0}}#dc2626{{else if gt .Delta 0.0}}#16a34a{{else}}#6b7280{{end}};">{{.DeltaText}}</td>
    <td style="padding:10px 12px;border-bottom:1px solid #f3f4f6;"><span style="padding:2px 8px;border-radius:10px;font-size:12px;background:{{.GradeBg}};color:#fff;">{{.Grade}}</span></td>
  </tr>{{end}}
  </tbody></table>
  {{if .Alerts}}
  <div style="margin-top:20px;"><div style="font-size:14px;font-weight:600;margin-bottom:10px;">⚠️ 本周异常</div>
  <ul style="margin:0;padding-left:20px;font-size:13px;line-height:1.9;">{{range .Alerts}}<li>{{.}}</li>{{end}}</ul></div>
  {{end}}
</td></tr>
<tr><td style="padding:14px 28px;background:#f9fafb;border-radius:0 0 8px 8px;font-size:12px;color:#6b7280;">
  由 my-geo 自动发送 · 前往控制台：<a href="{{.ConsoleURL}}" style="color:#2563eb;">{{.ConsoleURL}}</a>
</td></tr></table></body></html>`
)

// TemplateAlertData 告警模板数据。
type TemplateAlertData struct {
	Subject    string
	BrandName  string
	Severity   string // critical/warning/info
	Timestamp  string
	Message    string
	Extra      string
	ConsoleURL string
}

// TemplateWeeklyBrand 周报单品牌行。
type TemplateWeeklyBrand struct {
	Name      string
	Score     float64
	Delta     float64
	DeltaText string
	Grade     string
	GradeBg   string // CSS 颜色
}

// TemplateWeeklyData 周报模板数据。
type TemplateWeeklyData struct {
	Subject    string
	WeekRange  string
	Summary    string
	Brands     []TemplateWeeklyBrand
	Alerts     []string
	ConsoleURL string
}

// RenderAlertHTML 渲染告警邮件 HTML。
func RenderAlertHTML(d TemplateAlertData) (string, error) {
	return renderTemplate("alert", TemplateAlert, d)
}

// RenderWeeklyHTML 渲染周报邮件 HTML。
func RenderWeeklyHTML(d TemplateWeeklyData) (string, error) {
	// 预处理 grade 背景色
	gradeColor := map[string]string{
		"A": "#16a34a", "B": "#2563eb", "C": "#d97706",
		"D": "#ea580c", "E": "#dc2626", "F": "#7f1d1d",
	}
	for i := range d.Brands {
		if d.Brands[i].GradeBg == "" {
			if c, ok := gradeColor[d.Brands[i].Grade]; ok {
				d.Brands[i].GradeBg = c
			} else {
				d.Brands[i].GradeBg = "#6b7280"
			}
		}
	}
	return renderTemplate("weekly", TemplateWeekly, d)
}

func renderTemplate(name, src string, data any) (string, error) {
	t := template.New(name)
	if _, err := t.Parse(src); err != nil {
		return "", fmt.Errorf("mail: 模板解析失败 %s: %w", name, err)
	}
	var out bytes.Buffer
	if err := t.Execute(&out, data); err != nil {
		return "", fmt.Errorf("mail: 模板执行失败 %s: %w", name, err)
	}
	return out.String(), nil
}
