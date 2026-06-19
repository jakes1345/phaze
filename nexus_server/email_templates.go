package main

import (
	"html"
	"strings"
)

// emailBase wraps content in the shared Phaze dark-mode email shell.
func emailBase(preheader, content string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="dark">
<title>Phaze</title>
</head>
<body style="margin:0;padding:0;background:#0a0a0a;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Helvetica,Arial,sans-serif;color:#f5f5f7">
<span style="display:none;max-height:0;overflow:hidden">` + html.EscapeString(preheader) + `</span>
<table width="100%" cellpadding="0" cellspacing="0" style="background:#0a0a0a;padding:40px 16px">
  <tr><td align="center">
    <table width="560" cellpadding="0" cellspacing="0" style="max-width:560px;width:100%">

      <!-- Logo -->
      <tr><td style="padding-bottom:32px;text-align:center">
        <span style="font-size:22px;font-weight:800;letter-spacing:-0.04em;color:#f5f5f7">
          pha<span style="color:#a677ff">ze</span>
        </span>
      </td></tr>

      <!-- Card -->
      <tr><td style="background:#111;border:1px solid #1c1c1e;border-radius:20px;padding:40px 40px 36px">
        ` + content + `
      </td></tr>

      <!-- Footer -->
      <tr><td style="padding-top:28px;text-align:center;font-size:12px;color:#48484a;line-height:1.6">
        Phaze &nbsp;·&nbsp; <a href="https://phazechat.world" style="color:#636366;text-decoration:none">phazechat.world</a><br>
        You're receiving this because you have a Phaze account.
      </td></tr>

    </table>
  </td></tr>
</table>
</body>
</html>`
}

func primaryBtn(label, href string) string {
	return `<a href="` + html.EscapeString(href) + `" style="display:inline-block;background:#a677ff;color:#fff;text-decoration:none;font-weight:700;font-size:15px;padding:14px 32px;border-radius:12px;letter-spacing:-0.01em">` + html.EscapeString(label) + `</a>`
}

func mutedText(s string) string {
	return `<p style="margin:16px 0 0;font-size:13px;color:#636366;line-height:1.6">` + s + `</p>`
}

func codeBlock(code string) string {
	return `<div style="margin:24px 0;background:#0a0a0a;border:1px solid #2c2c2e;border-radius:12px;padding:20px;text-align:center">
    <span style="font-family:'JetBrains Mono','Courier New',monospace;font-size:28px;font-weight:700;letter-spacing:0.15em;color:#a677ff">` + html.EscapeString(code) + `</span>
  </div>`
}

// EmailVerification is sent on registration.
func emailVerification(username, verifyLink, code string) string {
	content := `<h1 style="margin:0 0 8px;font-size:24px;font-weight:800;letter-spacing:-0.03em;color:#f5f5f7">Verify your account</h1>
    <p style="margin:0 0 28px;font-size:15px;color:#98989d;line-height:1.6">Hey ` + html.EscapeString(username) + `, welcome to Phaze. Tap the button to activate your account.</p>
    <div style="text-align:center;margin:0 0 28px">` + primaryBtn("Activate Account", verifyLink) + `</div>` +
		mutedText(`Or enter this code in the app:`) +
		codeBlock(code) +
		mutedText(`This link expires in 24 hours. If you didn't create a Phaze account, you can ignore this email.`)
	return emailBase("Activate your Phaze account — one click and you're in.", content)
}

// EmailResendCode is sent when the user requests a new verification code.
func emailResendCode(code string) string {
	content := `<h1 style="margin:0 0 8px;font-size:24px;font-weight:800;letter-spacing:-0.03em;color:#f5f5f7">New activation code</h1>
    <p style="margin:0 0 4px;font-size:15px;color:#98989d;line-height:1.6">Here's your new code:</p>` +
		codeBlock(code) +
		mutedText(`Enter this in the Phaze app to verify your account. Expires in 24 hours.`)
	return emailBase("Your new Phaze activation code.", content)
}

// EmailPasswordReset is sent on forgot password.
func emailPasswordReset(username, resetLink string) string {
	content := `<h1 style="margin:0 0 8px;font-size:24px;font-weight:800;letter-spacing:-0.03em;color:#f5f5f7">Reset your password</h1>
    <p style="margin:0 0 28px;font-size:15px;color:#98989d;line-height:1.6">Hey ` + html.EscapeString(username) + `, we got a request to reset your Phaze password. Tap the button — this link is valid for <strong style="color:#f5f5f7">1 hour</strong>.</p>
    <div style="text-align:center;margin:0 0 28px">` + primaryBtn("Reset Password", resetLink) + `</div>` +
		mutedText(`Didn't request this? Your password is still safe — just ignore this email. If you're concerned, <a href="https://phazechat.world" style="color:#a677ff;text-decoration:none">contact us</a>.`)
	return emailBase("Reset your Phaze password.", content)
}

// EmailInvite is sent when a user invites someone.
func emailInvite(inviterUsername, joinLink string) string {
	display := html.EscapeString(strings.TrimSpace(inviterUsername))
	safeLink := html.EscapeString(joinLink)
	content := `<h1 style="margin:0 0 8px;font-size:24px;font-weight:800;letter-spacing:-0.03em;color:#f5f5f7">You've been invited</h1>
    <p style="margin:0 0 28px;font-size:15px;color:#98989d;line-height:1.6"><strong style="color:#f5f5f7">` + display + `</strong> wants to connect with you on Phaze — encrypted messaging, voice &amp; video calls, and more. It's free.</p>
    <div style="text-align:center;margin:0 0 28px">` + primaryBtn("Join Phaze", joinLink) + `</div>` +
		mutedText(`Or paste this link in your browser: <a href="` + safeLink + `" style="color:#a677ff;text-decoration:none">` + safeLink + `</a>`)
	return emailBase(strings.TrimSpace(inviterUsername)+" invited you to Phaze.", content)
}

// EmailSupporterThankYou is sent when someone submits the support form.
func emailSupporterThankYou(name string) string {
	content := `<h1 style="margin:0 0 8px;font-size:24px;font-weight:800;letter-spacing:-0.03em;color:#f5f5f7">Thank you 💜</h1>
    <p style="margin:0 0 20px;font-size:15px;color:#98989d;line-height:1.6">Hey ` + html.EscapeString(name) + `, your support means everything to us. Once your contribution is confirmed, your supporter badge will show up on your Phaze profile automatically.</p>
    <div style="background:#1a0a2e;border:1px solid #2d1a4a;border-radius:12px;padding:20px 24px;margin:0 0 20px">
      <span style="font-size:13px;font-weight:700;text-transform:uppercase;letter-spacing:0.08em;color:#a677ff">What happens next</span>
      <p style="margin:8px 0 0;font-size:14px;color:#98989d;line-height:1.6">We'll match your payment and grant the <strong style="color:#f5f5f7">💜 Supporter</strong> badge to your account. If you donated under a different name or email, just reply to this email so we can sort it out.</p>
    </div>` +
		mutedText(`Thanks again for keeping Phaze alive. — The Phaze team`)
	return emailBase("Thanks for supporting Phaze.", content)
}
