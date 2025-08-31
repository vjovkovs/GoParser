package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/vjovkovs/goparser/internal/fetch"
	"github.com/vjovkovs/goparser/internal/model"
	"github.com/vjovkovs/goparser/internal/parse"
)

// Options controls the crawl behavior.
type Options struct {
	MaxPages int  // maximum number of pages to follow starting at StartURL
	SaveEach bool // if true, persist each chapter via store.Save
}

// Saver is implemented by storage backends (e.g., internal/storage.FS).
type Saver interface {
	Save(ch model.Chapter) error
}

// RunResult is returned by Runner.Run and includes the collected chapters.
type RunResult struct {
	Chapters []model.Chapter
}

// Runner orchestrates fetch → parse → (optional) store.
type Runner struct {
	client *fetch.Client
	store  Saver
	opt    Options
	cfg    model.ParserConfig
}

// NewRunner constructs a Runner. If opt.MaxPages < 1, it defaults to 1.
func NewRunner(c *fetch.Client, s Saver, opt Options) *Runner {
	if opt.MaxPages < 1 {
		opt.MaxPages = 1
	}
	return &Runner{
		client: c,
		store:  s,
		opt:    opt,
		cfg:    model.DefaultParserConfig(),
	}
}

// WithParserConfig lets callers override the default parser config if needed.
func (r *Runner) WithParserConfig(cfg model.ParserConfig) *Runner {
	r.cfg = cfg
	return r
}

// Run performs the sequential crawl starting at startURL.
func (r *Runner) Run(ctx context.Context, startURL string) (RunResult, error) {
	current := startURL
	var all []model.Chapter

	for i := 0; i < r.opt.MaxPages && current != ""; i++ {
		// Fetch
		resp, err := r.client.Get(ctx, current)
		if err != nil {
			return RunResult{}, fmt.Errorf("get %s: %w", current, err)
		}
		doc, err := goquery.NewDocumentFromReader(resp.Body)
		resp.Body.Close()
		if err != nil {
			return RunResult{}, fmt.Errorf("parse html %s: %w", current, err)
		}

		// Parse
		ch, next, err := parse.ParseChapter(doc, current, r.cfg)
		if err != nil {
			return RunResult{}, fmt.Errorf("parse chapter %s: %w", current, err)
		}

		// Optionally persist each chapter
		if r.opt.SaveEach && r.store != nil {
			if err := r.store.Save(ch); err != nil {
				return RunResult{}, fmt.Errorf("save chapter %s: %w", ch.Title, err)
			}
		}

		all = append(all, ch)

		// Advance to next (resolve relative URLs if needed)
		if next == nil || strings.TrimSpace(*next) == "" {
			break
		}
		nxt := resolveURL(current, *next)
		if nxt == "" {
			break
		}
		current = nxt
	}

	return RunResult{Chapters: all}, nil
}

// resolveURL resolves href relative to base (if href is not absolute).
func resolveURL(baseStr, href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	h, herr := url.Parse(href)
	if herr != nil {
		return ""
	}
	// absolute already
	if h.Scheme != "" && h.Host != "" {
		return h.String()
	}
	// resolve relative
	bu, berr := url.Parse(baseStr)
	if berr != nil {
		return ""
	}
	return bu.ResolveReference(h).String()
}
