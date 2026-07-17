package http

import "html"

const placeholderHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>JKRT</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Noto+Sans+JP:wght@400;500;700&display=swap" rel="stylesheet">
  <style>
    body { font-family: "Noto Sans JP", ui-sans-serif, system-ui, sans-serif; }
    .primary { color: #3B82F6; }
  </style>
</head>
<body class="min-h-screen bg-slate-50 text-slate-900">
  <main class="mx-auto max-w-lg px-4 py-12">
    <h1 class="text-2xl font-bold primary">Japanese Kanji Reading Trainer</h1>
    <p class="mt-3 text-slate-600">Phase 0 placeholder. Review and scrape arrive in later phases.</p>
    <form method="post" action="/logout" class="mt-8">
      <button type="submit" class="rounded-lg bg-blue-500 px-4 py-2 text-sm font-medium text-white hover:bg-blue-600">
        Log out
      </button>
    </form>
  </main>
</body>
</html>
`

func loginHTML(errMsg string) string {
	errBlock := ""
	if errMsg != "" {
		errBlock = `<p class="mt-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">` +
			html.EscapeString(errMsg) + `</p>`
	}
	return `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Login — JKRT</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Noto+Sans+JP:wght@400;500;700&display=swap" rel="stylesheet">
  <style>
    body { font-family: "Noto Sans JP", ui-sans-serif, system-ui, sans-serif; }
  </style>
</head>
<body class="min-h-screen bg-slate-50 text-slate-900">
  <main class="mx-auto max-w-sm px-4 py-16">
    <h1 class="text-xl font-bold text-blue-500">JKRT</h1>
    <p class="mt-1 text-sm text-slate-600">Enter password to continue.</p>
    ` + errBlock + `
    <form method="post" action="/login" class="mt-6 space-y-4">
      <label class="block">
        <span class="text-sm font-medium text-slate-700">Password</span>
        <input type="password" name="password" required autofocus
          class="mt-1 w-full rounded-lg border border-slate-300 px-3 py-2 shadow-sm focus:border-blue-500 focus:outline-none focus:ring-1 focus:ring-blue-500">
      </label>
      <button type="submit"
        class="w-full rounded-lg bg-blue-500 px-4 py-2 text-sm font-medium text-white hover:bg-blue-600">
        Log in
      </button>
    </form>
  </main>
</body>
</html>
`
}
