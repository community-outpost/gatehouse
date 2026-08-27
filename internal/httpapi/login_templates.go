package httpapi

import "html/template"

type providerChoice struct {
	Path        string
	Label       string
	Description string
	Monogram    string
	HasIcon     bool
}

var loginPage = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<link rel="icon" href="/favicon.ico">
<title>{{.Brand.ServiceName}} · Sign in</title>
<style>` + loginPageStyles + `</style>
</head>
<body style="--accent: {{.Brand.AccentColor}}; --page: {{.Brand.BackgroundColor}}">
<main class="login-shell">
  <header class="brand-header">
    <img class="brand-logo" src="/assets/branding/logo" alt="{{.Brand.ServiceName}}">
    {{if .Brand.OperatorName}}<p>Operated by {{.Brand.OperatorName}}</p>{{end}}
  </header>

  <section class="login-card" aria-labelledby="login-title">
    <div class="card-heading">
      <h1 id="login-title">Sign in to {{.Brand.ApplicationName}}</h1>
      <p>Choose an account to continue.</p>
    </div>

    <p class="security-note">Only continue if you initiated this sign-in from the game.</p>

    <dl class="login-context" aria-label="Login destination">
      <div><dt>Environment</dt><dd>{{.Environment}}</dd></div>
    </dl>

    <div class="provider-list">
      {{range .Providers}}
      <a class="provider-card" href="/login/{{.Path}}?gamecode={{$.GameCode}}&amp;env={{$.Environment}}">
        <span class="provider-mark" aria-hidden="true">{{if .HasIcon}}<img src="/assets/providers/{{.Path}}" alt="">{{else}}{{.Monogram}}{{end}}</span>
        <span class="provider-copy"><strong>{{.Label}}</strong><small>{{.Description}}</small></span>
        <span class="provider-arrow" aria-hidden="true">→</span>
      </a>
      {{end}}
    </div>
  </section>
</main>
</body>
</html>`))

var displayNamePage = template.Must(template.New("display-name").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<link rel="icon" href="/favicon.ico">
<title>{{.Brand.ServiceName}} · Choose a display name</title>
<style>` + loginPageStyles + `</style>
<script src="/assets/display-name.js" defer></script>
</head>
<body style="--accent: {{.Brand.AccentColor}}; --page: {{.Brand.BackgroundColor}}">
<main class="login-shell compact-shell">
  <header class="brand-header">
    <img class="brand-logo" src="/assets/branding/logo" alt="{{.Brand.ServiceName}}">
    {{if .Brand.OperatorName}}<p>Operated by {{.Brand.OperatorName}}</p>{{end}}
  </header>

  <section class="login-card form-card" aria-labelledby="display-name-title">
    <div class="card-heading">
      <h1 id="display-name-title">Choose your display name</h1>
      <p>This is the name other players will see in {{.Brand.ApplicationName}}.</p>
    </div>
    {{if .Error}}<div class="form-error" role="alert">{{.Error}}</div>{{end}}
    <form method="post" class="display-form" data-display-name-form>
      <input type="hidden" name="csrf" value="{{.CSRF}}">
      <label for="display-name">Display name</label>
      <input id="display-name" name="display_name" value="{{.DisplayName}}" minlength="{{.MinLength}}" maxlength="{{.MaxLength}}" autocomplete="nickname" required autofocus placeholder="Enter a display name" aria-describedby="display-name-help display-name-validation">
      <div id="display-name-help" class="field-help"><span>Safe symbols: _ - . ( ) [ ] { } ! ? @ # $ % ^ &amp; * + = ~ '</span><span>{{.MinLength}}–{{.MaxLength}} characters</span></div>
      <p id="display-name-validation" class="validation-message" aria-live="polite"></p>
      <button type="button" class="random-name-button" data-random-name><span aria-hidden="true">↻</span> Roll a name</button>
      <button type="submit">Continue <span aria-hidden="true">→</span></button>
    </form>
    <p class="privacy-note">Names from your login provider are not copied.</p>
  </section>
</main>
</body>
</html>`))

var loginResultPage = template.Must(template.New("login-result").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="dark">
<link rel="icon" href="/favicon.ico">
<title>{{.Brand.ServiceName}} · {{.Title}}</title>
<style>` + loginPageStyles + `</style>
</head>
<body style="--accent: {{.Brand.AccentColor}}; --page: {{.Brand.BackgroundColor}}">
<main class="login-shell compact-shell">
  <header class="brand-header">
    <img class="brand-logo" src="/assets/branding/logo" alt="{{.Brand.ServiceName}}">
  </header>
  <section class="login-card result-card {{if .Success}}result-success{{else}}result-failure{{end}}">
    <div class="result-icon" aria-hidden="true">{{if .Success}}✓{{else}}!{{end}}</div>
    <h1>{{.Title}}</h1>
    <p>{{.Message}}</p>
    <small>{{if .Success}}This window can now be closed.{{else}}Return to {{.Brand.ApplicationName}} and try again.{{end}}</small>
  </section>
</main>
</body>
</html>`))

const loginPageStyles = `
:root {
  color-scheme: dark;
  --text: #f3f7f7;
  --muted: #8c999b;
  --surface: #101617;
  --surface-hover: #151d1e;
  --line: rgba(219, 246, 247, .12);
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}

* { box-sizing: border-box; }

body {
  min-height: 100vh;
  margin: 0;
  padding: 48px 20px;
  color: var(--text);
  background:
    radial-gradient(circle at 50% -16rem, color-mix(in srgb, var(--accent) 12%, transparent), transparent 38rem),
    var(--page);
}

.login-shell {
  width: min(520px, 100%);
  margin: 0 auto;
}

.brand-header {
  margin-bottom: 30px;
  text-align: center;
}

.brand-logo {
  display: block;
  width: auto;
  max-width: min(310px, 78vw);
  height: 84px;
  margin: 0 auto;
  object-fit: contain;
}

.brand-header p {
  margin: 9px 0 0;
  color: #697678;
  font-size: 11px;
  letter-spacing: .04em;
}

.login-card {
  overflow: hidden;
  padding: 30px;
  border: 1px solid var(--line);
  border-radius: 16px;
  background: var(--surface);
  box-shadow: 0 24px 60px rgba(0, 0, 0, .34);
}

.card-heading { margin-bottom: 22px; }

h1, p { margin-top: 0; }

.card-heading h1,
.result-card h1 {
  margin-bottom: 8px;
  font-size: 25px;
  line-height: 1.2;
  letter-spacing: -.025em;
}

.card-heading p,
.result-card > p {
  margin-bottom: 0;
  color: var(--muted);
  font-size: 14px;
  line-height: 1.55;
}

.login-context {
  display: flex;
  gap: 20px;
  margin: 0 0 22px;
  padding: 12px 14px;
  border: 1px solid rgba(219, 246, 247, .07);
  border-radius: 10px;
  background: rgba(255, 255, 255, .018);
}

.login-context div { min-width: 0; flex: 1; }
.login-context dt {
  margin-bottom: 4px;
  color: #687577;
  font-size: 9px;
  font-weight: 700;
  letter-spacing: .1em;
  text-transform: uppercase;
}
.login-context dd {
  margin: 0;
  overflow: hidden;
  color: #bac4c5;
  font: 600 11px ui-monospace, SFMono-Regular, Menlo, monospace;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.provider-list { display: grid; gap: 8px; }

.provider-card {
  display: grid;
  grid-template-columns: 38px 1fr 20px;
  align-items: center;
  gap: 13px;
  min-height: 62px;
  padding: 10px 14px;
  border: 1px solid var(--line);
  border-radius: 10px;
  color: var(--text);
  background: rgba(255, 255, 255, .018);
  text-decoration: none;
  transition: border-color .16s ease, background .16s ease;
}

.provider-card:hover {
  border-color: color-mix(in srgb, var(--accent) 52%, transparent);
  background: var(--surface-hover);
}

.provider-card:focus-visible,
input:focus-visible,
button:focus-visible {
  outline: 2px solid var(--accent);
  outline-offset: 2px;
}

.provider-mark {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  color: var(--accent);
  font-size: 10px;
  font-weight: 800;
}

.provider-mark img {
  display: block;
  width: 30px;
  height: 30px;
  object-fit: contain;
}

.provider-copy { min-width: 0; display: grid; gap: 3px; }
.provider-copy strong { font-size: 13px; font-weight: 650; }
.provider-copy small {
  overflow: hidden;
  color: #748184;
  font-size: 11px;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.provider-arrow { color: #657274; font-size: 17px; }
.provider-card:hover .provider-arrow { color: var(--accent); }

.security-note {
  position: relative;
  margin: 0 0 16px;
  padding: 12px 14px 12px 40px;
  border: 1px solid rgba(241, 176, 54, .4);
  border-radius: 9px;
  color: #f5d58f;
  background: rgba(241, 154, 35, .09);
  font-size: 12px;
  font-weight: 650;
  line-height: 1.45;
}

.security-note::before {
  content: "!";
  position: absolute;
  top: 11px;
  left: 14px;
  width: 17px;
  height: 17px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  color: #151006;
  background: #efb23e;
  font-size: 11px;
  font-weight: 900;
}

.privacy-note {
  margin: 20px 2px 0;
  color: #687577;
  font-size: 11px;
  line-height: 1.5;
}


.display-form { margin-top: 24px; }
.display-form label { display: block; margin-bottom: 8px; font-size: 12px; font-weight: 650; }
.display-form input {
  width: 100%;
  height: 52px;
  padding: 0 14px;
  border: 1px solid var(--line);
  border-radius: 9px;
  color: var(--text);
  background: #0b1011;
  font: inherit;
}
.display-form input::placeholder { color: #556164; }
.display-form input:focus { border-color: var(--accent); }
.field-help {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  margin: 8px 2px 0;
  color: #667275;
  font-size: 10px;
}
.validation-message {
  min-height: 17px;
  margin: 7px 2px 13px;
  font-size: 11px;
  line-height: 1.45;
}
.validation-valid { color: #74dca8; }
.validation-invalid { color: #ffad91; }
.display-form button[type="submit"] {
  width: 100%;
  height: 50px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 16px;
  border: 0;
  border-radius: 9px;
  color: #001012;
  background: var(--accent);
  cursor: pointer;
  font: 750 13px inherit;
}
.display-form button[type="submit"]:hover { filter: brightness(1.08); }
.display-form button[type="submit"]:disabled {
  opacity: .48;
  cursor: not-allowed;
  filter: none;
}
.random-name-button {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  margin: 0 0 12px;
  padding: 7px 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  color: #aebabb;
  background: rgba(255, 255, 255, .025);
  cursor: pointer;
  font: 650 11px inherit;
}
.random-name-button:hover {
  border-color: color-mix(in srgb, var(--accent) 45%, transparent);
  color: var(--text);
}

.form-error {
  margin-bottom: 18px;
  padding: 11px 13px;
  border: 1px solid rgba(255, 110, 110, .3);
  border-radius: 9px;
  color: #ffb4b4;
  background: rgba(255, 80, 80, .07);
  font-size: 12px;
  line-height: 1.45;
}

.result-card { text-align: center; }
.result-icon {
  width: 48px;
  height: 48px;
  display: grid;
  place-items: center;
  margin: 0 auto 20px;
  border: 1px solid color-mix(in srgb, var(--accent) 45%, transparent);
  border-radius: 50%;
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 7%, transparent);
  font-size: 20px;
  font-weight: 800;
}
.result-failure .result-icon {
  border-color: rgba(255, 110, 110, .35);
  color: #ff9393;
  background: rgba(255, 80, 80, .07);
}
.result-card small {
  display: block;
  margin-top: 24px;
  color: #687577;
  font-size: 11px;
}

@media (max-width: 520px) {
  body { padding: 28px 14px; }
  .brand-header { margin-bottom: 22px; }
  .brand-logo { height: 70px; }
  .login-card { padding: 23px 18px; border-radius: 13px; }
  .login-context { gap: 12px; }
  .provider-copy small { white-space: normal; }
}

@media (prefers-reduced-motion: reduce) {
  *, *::before, *::after { transition: none !important; }
}
`
