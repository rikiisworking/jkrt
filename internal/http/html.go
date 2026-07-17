package http

import (
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/rikiisworking/jkrt/internal/db"
	"github.com/rikiisworking/jkrt/internal/review"
	"github.com/rikiisworking/jkrt/internal/scrape"
)

// Primary brand blue (DEVELOPMENT_PLAN / AGENTS).
const primaryHex = "#3B82F6"

// Shared document head: Tailwind CDN, optional HTMX, Noto Sans JP, theme.
func docHead(title string, withHTMX bool) string {
	htmx := ""
	if withHTMX {
		htmx = `  <script src="https://unpkg.com/htmx.org@2.0.4"></script>
`
	}
	return `<!DOCTYPE html>
<html lang="ja">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>` + html.EscapeString(title) + `</title>
  <script src="https://cdn.tailwindcss.com"></script>
` + htmx + `  <link rel="preconnect" href="https://fonts.googleapis.com">
  <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
  <link href="https://fonts.googleapis.com/css2?family=Noto+Sans+JP:wght@400;500;700&display=swap" rel="stylesheet">
  <style>
    body { font-family: "Noto Sans JP", ui-sans-serif, system-ui, sans-serif; }
    .primary { color: ` + primaryHex + `; }
    .bg-primary { background-color: ` + primaryHex + `; }
    .unfamiliar { font-weight: 700; color: #1e40af; }
    strong.unfamiliar { background: #dbeafe; padding: 0 0.1em; border-radius: 0.15em; }
    rt { visibility: hidden; }
    body.show-furi rt { visibility: visible; }
    ruby { ruby-align: center; }
  </style>
</head>
`
}

func navHTML(active string) string {
	link := func(href, label, key string) string {
		cls := "text-sm text-slate-500 hover:text-blue-500"
		if active == key {
			cls = "text-sm font-medium text-blue-500"
		}
		return fmt.Sprintf(`<a href="%s" class="%s">%s</a>`, href, cls, label)
	}
	return `<nav class="flex flex-wrap items-center gap-4">
      ` + link("/", "Home", "home") + `
      ` + link("/review", "Review", "review") + `
      ` + link("/articles", "Articles", "articles") + `
    </nav>`
}

func pageShell(title, active, extraHead, mainInner string) string {
	return docHead(title, true) + extraHead + `<body class="min-h-screen bg-slate-50 text-slate-900">
  <div class="mx-auto max-w-lg px-4 py-8">
    <header class="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 pb-4">
      <a href="/" class="text-lg font-bold primary">JKRT</a>
      ` + navHTML(active) + `
    </header>
    <main class="mt-6">
` + mainInner + `
    </main>
    <footer class="mt-10 border-t border-slate-200 pt-4 text-center">
      <form method="post" action="/logout">
        <button type="submit" class="text-sm text-slate-400 hover:text-slate-600">Log out</button>
      </form>
    </footer>
  </div>
</body>
</html>`
}

// --- Dashboard ---

// DashboardData is the home page payload.
type DashboardData struct {
	Stats         review.Stats
	Library       db.LibraryCounts
	ArticleCount  int
	LastFetchedAt string // empty if never scraped
	HasLastFetch  bool
}

func dashboardHTML(d DashboardData) string {
	lastScrape := "Never"
	if d.HasLastFetch {
		lastScrape = formatFetchedDisplay(d.LastFetchedAt)
	}

	queueReady := d.Stats.DueCount + remainingNew(d.Stats)
	reviewCTA := "Review"
	if queueReady == 0 {
		reviewCTA = "Review (empty)"
	}

	phaseLine := fmt.Sprintf(
		"Cards: %d · words: %d · reviews: %d · mature: %d",
		d.Library.Cards, d.Library.Words, d.Library.Reviews, d.Library.MatureCards,
	)
	byPhase := fmt.Sprintf(
		"new %d · learning %d · review %d · relearning %d",
		d.Library.ByPhase["new"],
		d.Library.ByPhase["learning"],
		d.Library.ByPhase["review"],
		d.Library.ByPhase["relearning"],
	)

	inner := fmt.Sprintf(`
      <h1 class="text-2xl font-bold primary">Japanese Kanji Reading Trainer</h1>
      <p class="mt-2 text-sm text-slate-600">Kanji-bearing words in real news. Furigana stays hidden until you ask.</p>

      <section class="mt-6 grid grid-cols-2 gap-3">
        <div class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
          <p class="text-xs uppercase tracking-wide text-slate-400">Due</p>
          <p class="mt-1 text-2xl font-bold text-slate-900">%d</p>
        </div>
        <div class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
          <p class="text-xs uppercase tracking-wide text-slate-400">New in queue</p>
          <p class="mt-1 text-2xl font-bold text-slate-900">%d</p>
        </div>
      </section>

      <section class="mt-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
        <p class="text-xs uppercase tracking-wide text-slate-400">Session progress (UTC day)</p>
        <div class="mt-3 space-y-3">
          %s
          %s
        </div>
      </section>

      <section class="mt-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
        <div class="flex items-start justify-between gap-3">
          <div>
            <p class="text-xs uppercase tracking-wide text-slate-400">Library</p>
            <p class="mt-1 text-sm text-slate-800"><span class="font-semibold">%d</span> articles · %d sentences</p>
            <p class="mt-1 text-xs text-slate-600">%s</p>
            <p class="mt-0.5 text-xs text-slate-500">%s</p>
            <p class="mt-1 text-xs text-slate-500">Last scrape: %s</p>
          </div>
          <a href="/articles" class="shrink-0 text-sm font-medium text-blue-500 hover:text-blue-600">Browse →</a>
        </div>
      </section>

      <section class="mt-4 rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
        <p class="text-xs uppercase tracking-wide text-slate-400">Export</p>
        <p class="mt-1 text-xs text-slate-500">Backup Cards / Reviews (auth required).</p>
        <div class="mt-3 flex flex-wrap gap-3">
          <a href="/api/export?format=json" class="text-sm font-medium text-blue-500 hover:text-blue-600">JSON</a>
          <a href="/api/export?format=csv" class="text-sm font-medium text-blue-500 hover:text-blue-600">CSV (cards)</a>
          <a href="/api/stats" class="text-sm font-medium text-slate-500 hover:text-slate-700">Stats JSON</a>
        </div>
      </section>

      <div class="mt-6 flex flex-wrap gap-3">
        <a href="/review"
          class="inline-flex items-center justify-center rounded-xl bg-primary px-5 py-3 text-sm font-semibold text-white shadow hover:bg-blue-600 active:scale-[0.98]">
          %s
        </a>
        <button type="button"
          class="inline-flex items-center justify-center rounded-xl border border-slate-300 bg-white px-5 py-3 text-sm font-medium text-slate-700 shadow-sm hover:bg-slate-50 active:scale-[0.98]"
          hx-post="/api/scrape" hx-target="#scrape-result" hx-swap="innerHTML"
          hx-disabled-elt="this">
          Scrape NHK
        </button>
      </div>
      <div id="scrape-result" class="mt-3" aria-live="polite"></div>
      %s
`,
		d.Stats.DueCount,
		d.Stats.NewCount,
		progressBar("Reviews today", d.Stats.ReviewsToday, d.Stats.SessionLimit),
		progressBar("New introduced", d.Stats.NewIntroducedToday, d.Stats.NewPerDay),
		d.ArticleCount,
		d.Library.Sentences,
		html.EscapeString(phaseLine),
		html.EscapeString(byPhase),
		html.EscapeString(lastScrape),
		html.EscapeString(reviewCTA),
		dashboardEmptyHint(d),
	)
	return pageShell("JKRT", "home", "", inner)
}

func remainingNew(st review.Stats) int {
	left := st.NewPerDay - st.NewIntroducedToday
	if left < 0 {
		left = 0
	}
	if st.NewCount < left {
		return st.NewCount
	}
	return left
}

func progressBar(label string, cur, max int) string {
	if max <= 0 {
		max = 1
	}
	pct := cur * 100 / max
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return fmt.Sprintf(`
          <div>
            <div class="flex justify-between text-sm">
              <span class="text-slate-600">%s</span>
              <span class="font-medium text-slate-800">%d / %d</span>
            </div>
            <div class="mt-1.5 h-2 overflow-hidden rounded-full bg-slate-100">
              <div class="h-full rounded-full bg-primary transition-all" style="width:%d%%"></div>
            </div>
          </div>`,
		html.EscapeString(label), cur, max, pct)
}

func dashboardEmptyHint(d DashboardData) string {
	if d.ArticleCount > 0 {
		return ""
	}
	return `
      <div class="mt-6 rounded-xl border border-dashed border-slate-300 bg-white/60 p-5 text-center">
        <p class="text-sm font-medium text-slate-700">No articles yet</p>
        <p class="mt-1 text-xs text-slate-500">Tap <strong>Scrape NHK</strong> to pull RSS into the library, then open <strong>Articles</strong> and add sentences to review.</p>
      </div>`
}

func formatFetchedDisplay(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "—"
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		// Try common SQLite-ish format
		t, err = time.Parse("2006-01-02 15:04:05", raw)
		if err != nil {
			return raw
		}
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

func scrapeResultHTML(r scrape.Result) string {
	var b strings.Builder
	b.WriteString(`<div class="rounded-xl border border-slate-200 bg-white p-4 text-sm shadow-sm">`)
	b.WriteString(`<p class="font-medium text-slate-800">Scrape finished</p><ul class="mt-2 space-y-1">`)
	for _, s := range r.Sources {
		name := html.EscapeString(s.Name)
		if s.OK {
			b.WriteString(fmt.Sprintf(
				`<li class="text-emerald-700">%s — ok, %d new</li>`, name, s.ItemsNew))
		} else {
			errMsg := html.EscapeString(s.Error)
			if errMsg == "" {
				errMsg = "failed"
			}
			b.WriteString(fmt.Sprintf(
				`<li class="text-red-600">%s — %s</li>`, name, errMsg))
		}
	}
	b.WriteString(`</ul>`)
	b.WriteString(`<p class="mt-3 text-xs text-slate-500">Reload home for updated counts, or open Articles.</p>`)
	b.WriteString(`</div>`)
	return b.String()
}

// --- Articles browse ---

func articlesListHTML(items []db.ArticleListItem) string {
	var body strings.Builder
	body.WriteString(`<h1 class="text-xl font-bold text-slate-900">Articles</h1>`)
	body.WriteString(`<p class="mt-1 text-sm text-slate-500">Browse scraped news and their Sentences.</p>`)

	if len(items) == 0 {
		body.WriteString(`
      <div class="mt-8 rounded-xl border border-dashed border-slate-300 bg-white p-8 text-center shadow-sm">
        <p class="text-lg font-medium text-slate-800">No articles</p>
        <p class="mt-2 text-sm text-slate-500">Scrape NHK feeds from the home dashboard first.</p>
        <a href="/" class="mt-6 inline-block rounded-lg bg-primary px-4 py-2 text-sm font-medium text-white hover:bg-blue-600">Home</a>
      </div>`)
		return pageShell("Articles — JKRT", "articles", "", body.String())
	}

	body.WriteString(`<ul class="mt-6 space-y-3">`)
	for _, it := range items {
		title := it.Title
		if strings.TrimSpace(title) == "" {
			title = "(untitled)"
		}
		body.WriteString(fmt.Sprintf(`
      <li>
        <a href="/articles/%d"
          class="block rounded-xl border border-slate-200 bg-white p-4 shadow-sm hover:border-blue-300 hover:shadow transition">
          <p class="font-medium text-slate-900 leading-snug" lang="ja">%s</p>
          <p class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-slate-500">
            <span>%s</span>
            <span>%d sentences</span>
            <span>%s</span>
          </p>
        </a>
      </li>`,
			it.ID,
			html.EscapeString(title),
			html.EscapeString(it.SourceName),
			it.SentenceCount,
			html.EscapeString(formatFetchedDisplay(it.FetchedAt)),
		))
	}
	body.WriteString(`</ul>`)
	return pageShell("Articles — JKRT", "articles", "", body.String())
}

func articleDetailHTML(art db.ArticleDetail, sents []db.SentenceListItem) string {
	title := art.Title
	if strings.TrimSpace(title) == "" {
		title = "(untitled)"
	}
	var body strings.Builder
	body.WriteString(fmt.Sprintf(`
      <p class="text-xs text-slate-400"><a href="/articles" class="hover:text-blue-500">← Articles</a></p>
      <h1 class="mt-2 text-xl font-bold text-slate-900 leading-snug" lang="ja">%s</h1>
      <p class="mt-2 flex flex-wrap gap-x-3 gap-y-1 text-xs text-slate-500">
        <span>%s</span>
        <span>%s</span>
      </p>
      <p class="mt-3 text-sm text-slate-600">Tap <strong>Add to review</strong> on a sentence to create Cards for its kanji words. Scrape alone does not fill the queue.</p>`,
		html.EscapeString(title),
		html.EscapeString(art.SourceName),
		html.EscapeString(formatFetchedDisplay(art.FetchedAt)),
	))
	if art.URL != "" {
		body.WriteString(fmt.Sprintf(
			`<p class="mt-2"><a href="%s" class="text-sm text-blue-500 hover:underline break-all" rel="noopener noreferrer" target="_blank">%s</a></p>`,
			html.EscapeString(art.URL),
			html.EscapeString(art.URL),
		))
	}

	if len(sents) == 0 {
		body.WriteString(`
      <div class="mt-8 rounded-xl border border-dashed border-slate-300 bg-white p-6 text-center">
        <p class="text-sm text-slate-600">No sentences stored for this article.</p>
      </div>`)
	} else {
		body.WriteString(`<ol class="mt-6 space-y-3 list-none">`)
		for _, s := range sents {
			body.WriteString(sentenceRowHTML(art.ID, s, db.ExtractResult{}))
		}
		body.WriteString(`</ol>`)
	}
	return pageShell(title+" — JKRT", "articles", "", body.String())
}

// sentenceRowHTML is one sentence list item (full page or HTMX partial after extract).
func sentenceRowHTML(articleID int64, s db.SentenceListItem, last db.ExtractResult) string {
	idAttr := fmt.Sprintf("sentence-%d", s.ID)
	meta := ""
	if last.SentenceID == s.ID {
		if last.Candidates == 0 {
			meta = `<p class="mt-1 text-xs text-slate-500">No kanji words to study in this sentence.</p>`
		} else if last.AlreadyExtracted && last.CardsNew == 0 {
			meta = `<p class="mt-1 text-xs text-slate-500">Already in review.</p>`
		} else if last.CardsNew > 0 {
			meta = fmt.Sprintf(`<p class="mt-1 text-xs text-emerald-600">Added %d new card(s).</p>`, last.CardsNew)
		} else {
			meta = `<p class="mt-1 text-xs text-emerald-600">Words linked to review.</p>`
		}
	}

	if s.Extracted {
		// "In queue" only when Words/Cards were linked; zero-candidate extracts are marked only.
		inQueue := s.WordCount > 0 || (last.SentenceID == s.ID && last.Candidates > 0)
		badge := `<span class="rounded-full bg-slate-100 px-2 py-0.5 text-xs font-medium text-slate-600">No study words</span>`
		if inQueue {
			badge = `<span class="rounded-full bg-emerald-50 px-2 py-0.5 text-xs font-medium text-emerald-700">In queue</span>`
		}
		return fmt.Sprintf(`
      <li id="%s" class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
        <div class="flex flex-wrap items-start justify-between gap-2">
          <p class="text-xs text-slate-400">Sentence</p>
          %s
        </div>
        <p class="mt-1 text-base leading-relaxed text-slate-900" lang="ja">%s</p>
        %s
      </li>`,
			idAttr,
			badge,
			html.EscapeString(s.Text),
			meta,
		)
	}

	return fmt.Sprintf(`
      <li id="%s" class="rounded-xl border border-slate-200 bg-white p-4 shadow-sm">
        <p class="text-xs text-slate-400">Sentence</p>
        <p class="mt-1 text-base leading-relaxed text-slate-900" lang="ja">%s</p>
        %s
        <form method="post" action="/articles/%d/sentences/%d/extract"
          hx-post="/articles/%d/sentences/%d/extract"
          hx-target="#%s" hx-swap="outerHTML"
          class="mt-3">
          <button type="submit"
            class="rounded-lg bg-primary px-3 py-1.5 text-sm font-medium text-white shadow hover:bg-blue-600 active:scale-[0.98]">
            Add to review
          </button>
        </form>
      </li>`,
		idAttr,
		html.EscapeString(s.Text),
		meta,
		articleID, s.ID,
		articleID, s.ID,
		idAttr,
	)
}

func articleNotFoundHTML() string {
	inner := `
      <div class="rounded-xl border border-slate-200 bg-white p-8 text-center shadow-sm">
        <p class="text-lg font-medium text-slate-800">Article not found</p>
        <a href="/articles" class="mt-6 inline-block rounded-lg bg-primary px-4 py-2 text-sm font-medium text-white hover:bg-blue-600">Back to articles</a>
      </div>`
	return pageShell("Not found — JKRT", "articles", "", inner)
}

// --- Review (Phase 3 shell + partials) ---

const reviewFuriScript = `
  <script>
    (function () {
      function applyFuri() {
        var on = sessionStorage.getItem('jkrt-furi') === '1';
        document.body.classList.toggle('show-furi', on);
        var el = document.getElementById('furi-toggle');
        if (el) el.checked = on;
      }
      document.addEventListener('DOMContentLoaded', applyFuri);
      document.addEventListener('htmx:afterSwap', applyFuri);
      window.jkrtToggleFuri = function (checked) {
        sessionStorage.setItem('jkrt-furi', checked ? '1' : '0');
        document.body.classList.toggle('show-furi', checked);
      };
      document.addEventListener('submit', function (e) {
        var form = e.target;
        if (!form || form.getAttribute('data-review-grade') !== '1') return;
        var buttons = form.querySelectorAll('button[type="submit"]');
        for (var i = 0; i < buttons.length; i++) {
          buttons[i].disabled = true;
        }
      }, true);
    })();
  </script>
`

func reviewPageShell(mainInner string) string {
	extra := reviewFuriScript
	// Review keeps its own header (furigana toggle) but shares shell chrome.
	inner := `
    <div class="flex items-center justify-between gap-2 mb-2">
      <h1 class="text-xl font-bold primary">Review</h1>
      <label class="flex items-center gap-1.5 text-sm text-slate-600 cursor-pointer select-none">
        <input type="checkbox" id="furi-toggle" class="rounded border-slate-300 text-blue-500 focus:ring-blue-500"
          onchange="jkrtToggleFuri(this.checked)">
        Furigana
      </label>
    </div>
    <div id="review-main">
` + mainInner + `
    </div>`
	// Use pageShell but active=review; skip double footer title by embedding.
	return pageShell("Review — JKRT", "review", extra, inner)
}

func reviewEmptyHTML() string {
	return reviewPageShell(reviewEmptyPartial())
}

func reviewEmptyPartial() string {
	return `      <div class="mt-6 rounded-xl border border-slate-200 bg-white p-8 text-center shadow-sm">
        <p class="text-lg font-medium text-slate-800">Queue empty</p>
        <p class="mt-2 text-sm text-slate-500">No due or new Cards. Scrape news if needed, then open <strong>Articles</strong> and tap <strong>Add to review</strong> on a sentence.</p>
        <div class="mt-6 flex flex-wrap justify-center gap-3">
          <a href="/" class="inline-block rounded-lg bg-primary px-4 py-2 text-sm font-medium text-white hover:bg-blue-600">Home</a>
          <a href="/articles" class="inline-block rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-100">Articles</a>
        </div>
      </div>`
}

func reviewHTML(item review.Item, errMsg string) string {
	return reviewPageShell(reviewPartial(item, errMsg))
}

// reviewPartial is the HTMX-swappable #review-main fragment (no doctype/head).
func reviewPartial(item review.Item, errMsg string) string {
	errBlock := ""
	if errMsg != "" {
		errBlock = `<p class="mt-3 rounded-md bg-red-50 px-3 py-2 text-sm text-red-700" role="alert">` +
			html.EscapeString(errMsg) + `</p>`
	}

	sentenceHTML := renderSentenceSpans(item.Spans)

	return fmt.Sprintf(`%s
      <section class="mt-2 rounded-xl border border-slate-200 bg-white p-5 shadow-sm">
        <p class="text-xs uppercase tracking-wide text-slate-400">Sentence</p>
        <p class="mt-3 text-xl leading-relaxed text-slate-900" lang="ja">%s</p>
        <div class="mt-5 border-t border-slate-100 pt-4">
          <p class="text-xs uppercase tracking-wide text-slate-400">Focus word</p>
          <p class="mt-1 text-lg font-medium text-slate-800">
            <ruby class="unfamiliar">%s<rt>%s</rt></ruby>
          </p>
          <p class="mt-1 text-xs text-slate-400">phase: %s · card #%d</p>
        </div>
      </section>
      <form method="post" action="/review" data-review-grade="1"
        hx-post="/review" hx-target="#review-main" hx-swap="innerHTML"
        class="mt-6 grid grid-cols-2 gap-3 sm:grid-cols-4">
        <input type="hidden" name="card_id" value="%d">
        <input type="hidden" name="sentence_id" value="%d">
        <input type="hidden" name="card_updated_at" value="%s">
        <button type="submit" name="grade" value="again"
          class="rounded-xl bg-red-500 px-3 py-3 text-sm font-semibold text-white shadow hover:bg-red-600 active:scale-[0.98] disabled:opacity-50">Again</button>
        <button type="submit" name="grade" value="hard"
          class="rounded-xl bg-orange-400 px-3 py-3 text-sm font-semibold text-white shadow hover:bg-orange-500 active:scale-[0.98] disabled:opacity-50">Hard</button>
        <button type="submit" name="grade" value="good"
          class="rounded-xl bg-primary px-3 py-3 text-sm font-semibold text-white shadow hover:bg-blue-600 active:scale-[0.98] disabled:opacity-50">Good</button>
        <button type="submit" name="grade" value="easy"
          class="rounded-xl bg-emerald-500 px-3 py-3 text-sm font-semibold text-white shadow hover:bg-emerald-600 active:scale-[0.98] disabled:opacity-50">Easy</button>
      </form>`,
		errBlock,
		sentenceHTML,
		html.EscapeString(item.Lemma),
		html.EscapeString(item.Reading),
		html.EscapeString(item.Phase),
		item.CardID,
		item.CardID,
		item.SentenceID,
		html.EscapeString(item.UpdatedAt),
	)
}

func renderSentenceSpans(spans []review.Span) string {
	var b strings.Builder
	for _, sp := range spans {
		surf := html.EscapeString(sp.Surface)
		if sp.WordID == 0 {
			b.WriteString(surf)
			continue
		}
		reading := html.EscapeString(sp.Reading)
		inner := surf
		if sp.Reading != "" {
			inner = "<ruby>" + surf + "<rt>" + reading + "</rt></ruby>"
		}
		if sp.Focus {
			b.WriteString(`<strong class="unfamiliar">`)
			b.WriteString(inner)
			b.WriteString(`</strong>`)
		} else if sp.Unfamiliar {
			b.WriteString(`<span class="unfamiliar">`)
			b.WriteString(inner)
			b.WriteString(`</span>`)
		} else {
			b.WriteString(inner)
		}
	}
	return b.String()
}

// --- Login / placeholder ---

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
    <p class="mt-3 text-slate-600">Review kanji-bearing words in news sentence context.</p>
    <div class="mt-6 flex flex-wrap gap-3">
      <a href="/review" class="rounded-lg bg-blue-500 px-4 py-2 text-sm font-medium text-white hover:bg-blue-600">Review</a>
    </div>
    <form method="post" action="/logout" class="mt-8">
      <button type="submit" class="rounded-lg border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-100">
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
