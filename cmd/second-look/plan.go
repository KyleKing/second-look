package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/kyleking/second-look/internal/artifact"
	"github.com/kyleking/second-look/internal/diff"
	"github.com/kyleking/second-look/internal/humanize"
	"github.com/kyleking/second-look/internal/plan"
)

var (
	errUsagePlan = errors.New("usage: second-look plan claim|stop|chapter|mode|ask <pr>; " +
		"second-look plan for the shapes")
	errPlanField = errors.New("the batch carries a field the verb does not read")
)

const planHelp = `second-look plan — stage the reading plan's claim, stops, chapters, and asks.

  Each verb reads one JSON object on stdin, like comment add, and writes the
  review's local [plan] table. Nothing in it is ever posted. Every path and line
  names a line of the cached diff, which is quoted so the entry follows it when
  the head moves; a line the diff does not carry is refused and nothing is written.

  second-look plan claim <pr>     {"claim": "what the change says it does"}
  second-look plan stop <pr>      {"stops": [{"id", "path", "line", "side",
                                    "start_line", "role", "what", "then"}]}
  second-look plan chapter <pr>   {"chapters": [{"chapter", "name", "mode", "why",
                                    "feature", "hunks": [{"path", "line", "side"}]}]}
  second-look plan mode <pr>      {"chapter": "name", "mode": "skim"}
  second-look plan ask <pr>       {"asks": [{"id", "path", "line", "side",
                                    "start_line", "question", "answer"}]}

  A stop or an ask with an id already staged replaces it, and a chapter entry
  replaces what was staged for the chapter it names; mode changes the mode alone.
  chapter is the name the plan gave the chapter, or a new name, which starts a
  chapter around the hunks it moves. mode is read, skim, shape, table, or fold.
`

func planCmd(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		return write(stdout, planHelp)
	}

	verbs := map[string]func(*artifact.Review, *json.Decoder) (string, error){
		"claim": stageClaim, "stop": stageStops, "chapter": stageChapters, "mode": stageMode, "ask": stageAsks,
	}

	verb, ok := verbs[args[0]]
	if !ok || len(args) != 2 {
		return errUsagePlan
	}

	t, err := target(ctx, args[1])
	if err != nil {
		return err
	}

	path := artifact.Path(t.Store, t.Number)

	r, err := artifact.Load(path)
	if err != nil {
		return fmt.Errorf("loading the prepared review: %w", err)
	}

	staged := *r
	staged.Plan = clonePlan(r.Plan)

	dec := json.NewDecoder(stdin)
	dec.DisallowUnknownFields()

	said, err := verb(&staged, dec)
	if err != nil {
		return fmt.Errorf("reading the batch: %w", err)
	}

	if err := staged.Validate(); err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written:\n%w", err)
	}

	cached, err := artifact.LoadDiff(t.Store, staged.HeadSHA)
	if err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written: %w", err)
	}

	if err := artifact.ResolvePlan(&staged.Plan, diff.Parse(cached)); err != nil {
		return fmt.Errorf("the batch was rejected and nothing was written:\n%w", err)
	}

	if err := artifact.Save(path, &staged); err != nil {
		return fmt.Errorf("saving the prepared review: %w", err)
	}

	return write(stdout, said+"\n")
}

func clonePlan(p artifact.Plan) artifact.Plan {
	p.Stops = slices.Clone(p.Stops)
	p.Asks = slices.Clone(p.Asks)
	p.Chapters = slices.Clone(p.Chapters)

	for i := range p.Chapters {
		p.Chapters[i].Hunks = slices.Clone(p.Chapters[i].Hunks)
	}

	return p
}

func stageClaim(r *artifact.Review, dec *json.Decoder) (string, error) {
	var b struct {
		Claim string `json:"claim"`
	}

	if err := dec.Decode(&b); err != nil {
		return "", fmt.Errorf("decoding the claim: %w", err)
	}

	r.Plan.Claim = b.Claim

	return "claim staged", nil
}

func stageStops(r *artifact.Review, dec *json.Decoder) (string, error) {
	return stageList(r, dec, "stops", "stop", &r.Plan.Stops,
		func(x *artifact.PlanStop) string { return x.ID },
		func(x *artifact.PlanStop) *string { return &x.Origin })
}

func stageChapters(r *artifact.Review, dec *json.Decoder) (string, error) {
	return stageList(r, dec, "chapters", "chapter edit", &r.Plan.Chapters,
		func(x *artifact.PlanChapter) string { return x.Chapter },
		func(x *artifact.PlanChapter) *string { return &x.Origin })
}

func stageAsks(r *artifact.Review, dec *json.Decoder) (string, error) {
	return stageList(r, dec, "asks", "ask", &r.Plan.Asks,
		func(x *artifact.PlanAsk) string { return x.ID },
		func(x *artifact.PlanAsk) *string { return &x.Origin })
}

// stageList reads {field: [entries]} and upserts each entry by its key.
func stageList[T any](r *artifact.Review, dec *json.Decoder, field, noun string, list *[]T,
	key func(*T) string, originOf func(*T) *string,
) (string, error) {
	var b map[string][]T

	if err := dec.Decode(&b); err != nil {
		return "", fmt.Errorf("decoding the %s: %w", field, err)
	}

	for k := range b {
		if k != field {
			return "", fmt.Errorf("%w: %q, want %q", errPlanField, k, field)
		}
	}

	for i := range b[field] {
		entry := &b[field][i]
		*list = upsert(r, *list, *entry, func(x T) bool { return key(&x) == key(entry) }, originOf)
	}

	return fmt.Sprintf("%s staged, %d total", humanize.Plural(len(b[field]), noun), len(*list)), nil
}

func stageMode(r *artifact.Review, dec *json.Decoder) (string, error) {
	var b struct {
		Chapter string    `json:"chapter"`
		Mode    plan.Mode `json:"mode"`
	}

	if err := dec.Decode(&b); err != nil {
		return "", fmt.Errorf("decoding the mode: %w", err)
	}

	same := func(c artifact.PlanChapter) bool { return c.Chapter == b.Chapter }

	edit := artifact.PlanChapter{Chapter: b.Chapter}
	if at := slices.IndexFunc(r.Plan.Chapters, same); at >= 0 {
		edit = r.Plan.Chapters[at]
	}

	edit.Mode = b.Mode
	r.Plan.Chapters = upsert(r, r.Plan.Chapters, edit, same,
		func(x *artifact.PlanChapter) *string { return &x.Origin })

	return fmt.Sprintf("%s reads as %s", b.Chapter, b.Mode), nil
}

// upsert replaces the entry same picks out, or appends one, attributing it the way
// a staged comment is.
func upsert[T any](r *artifact.Review, entries []T, entry T, same func(T) bool, originOf func(*T) *string) []T {
	at := slices.IndexFunc(entries, same)

	was := ""
	if at >= 0 {
		was = *originOf(&entries[at])
	}

	*originOf(&entry) = origin(r, was)

	if at >= 0 {
		entries[at] = entry

		return entries
	}

	return append(entries, entry)
}
