package post

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/kyleking/second-look/internal/artifact"
)

// attachRef is ![alt](attach:path): a body names a local file instead of a URL
// because user-attachments links only exist after upload, which happens here at
// the last moment a body can still change. A path with spaces goes in angle
// brackets, markdown's own spelling of a URL that is not whitespace-safe.
var attachRef = regexp.MustCompile(`!\[([^\]]*)\]\(attach:(?:<([^>]+)>|([^)\s]+))\)`)

// attachLike catches the same spelling written badly — a space left outside
// the brackets, an empty path — which the real ref passes over and a post
// would carry verbatim onto GitHub where it renders as a broken image.
var attachLike = regexp.MustCompile(`!\[[^\]]*\]\(attach:`)

// errBadAttach names an attach: reference the expander could not parse.
var errBadAttach = errors.New("a path with spaces has to be wrapped: attach:<path>")

// attachable maps the extensions user-attachments renders inline to their
// content type and the size GitHub accepts. Anything else is refused at read
// time, because uploads.github.com answers a wrong type with an upload that
// still succeeds and a comment that shows a broken image.
var attachable = map[string]struct {
	mime  string
	limit int64
}{
	".png":  {"image/png", 10 << 20},
	".jpg":  {"image/jpeg", 10 << 20},
	".jpeg": {"image/jpeg", 10 << 20},
	".gif":  {"image/gif", 10 << 20},
	".webp": {"image/webp", 10 << 20},
	".mp4":  {"video/mp4", 100 << 20},
	".mov":  {"video/quicktime", 100 << 20},
}

// errNotAttachable names a file whose extension user-attachments will not
// render, so the post stops at the body that names it.
var errNotAttachable = errors.New("not an image or video user-attachments renders")

// errAttachTooBig says the file is over what the upload endpoint takes.
var errAttachTooBig = errors.New("too big to upload as an attachment")

// errNoUploadURL says the upload endpoint answered without the url the whole
// call exists to get.
var errNoUploadURL = errors.New("the upload answered no url")

// keyName is the upload endpoint's name for the file being sent, a constant
// because GraphQL's "name" variable is spelled the same and only one of them
// is a variable.
const keyName = "name"

// mentionsAttach reports whether any body about to print still names a local
// file, which is what a dry run shows where the real run shows URLs. It matches
// the looser spelling on purpose so a malformed ref is said rather than missed.
func mentionsAttach(
	body string, threads []artifact.DraftThread, replies []artifact.ReplyPayload,
) bool {
	if attachLike.MatchString(body) {
		return true
	}
	for i := range threads {
		if attachLike.MatchString(threads[i].Body) {
			return true
		}
	}
	for i := range replies {
		if attachLike.MatchString(replies[i].Body) {
			return true
		}
	}

	return false
}

// expandAttachments rewrites the bodies a post is about to send: every
// ![alt](attach:path) becomes the user-attachments URL its file uploads to.
// The repository id is asked for only when a body actually carries a ref, so a
// post without attachments costs no extra call. Uploaded paths are memoized,
// since the same screenshot can be named from two comments.
func expandAttachments(
	ctx context.Context, p Poster, r *artifact.Review, root string,
	threads []artifact.DraftThread, replies []artifact.ReplyPayload,
) error {
	if !mentionsAttach(r.Body, threads, replies) {
		return nil
	}

	repoID, err := repoID(ctx, p, r.Owner, r.Repo)
	if err != nil {
		return err
	}

	u := &expander{p: p, host: reviewHost(r), repoID: repoID, root: root, seen: map[string]string{}}

	if err := u.body(ctx, &r.Body); err != nil {
		return fmt.Errorf("the review body: %w", err)
	}
	for i := range threads {
		if err := u.body(ctx, &threads[i].Body); err != nil {
			return fmt.Errorf("%s:%d: %w", threads[i].Path, threads[i].Line, err)
		}
	}
	for i := range replies {
		if err := u.body(ctx, &replies[i].Body); err != nil {
			return fmt.Errorf("reply %s: %w", replies[i].ID, err)
		}
	}

	return nil
}

// Expand rewrites one body's attach: references the way expandAttachments
// rewrites a review's, for the comment paths that post outside a review.
func Expand(ctx context.Context, p Poster, host, owner, repo, root string, body *string) error {
	if !attachLike.MatchString(*body) {
		return nil
	}

	repoID, err := repoID(ctx, p, owner, repo)
	if err != nil {
		return err
	}

	u := &expander{p: p, host: host, repoID: repoID, root: root, seen: map[string]string{}}

	return u.body(ctx, body)
}

// expander carries what rewriting a body needs after the one-time fetches are
// done: the transport, the repository id the upload names, the root relative
// paths resolve under, and the uploads already sent this run.
type expander struct {
	p      Poster
	host   string
	repoID int64
	root   string
	seen   map[string]string
}

func (e *expander) body(ctx context.Context, body *string) error {
	matches := attachRef.FindAllStringSubmatchIndex(*body, -1)

	var out strings.Builder

	last := 0

	for _, m := range matches {
		out.WriteString((*body)[last:m[0]])

		// The bracketed alternative is group 2, the bare one group 3, and
		// exactly one of them matched.
		lo, hi := m[6], m[7]
		if m[4] >= 0 {
			lo, hi = m[4], m[5]
		}

		path := (*body)[lo:hi]

		href, err := e.upload(ctx, path)
		if err != nil {
			return err
		}

		alt := (*body)[m[2]:m[3]]
		if alt == "" {
			alt = filepath.Base(path)
		}

		out.WriteString("![" + alt + "](" + href + ")")

		last = m[1]
	}

	out.WriteString((*body)[last:])
	*body = out.String()

	if loc := attachLike.FindStringIndex(*body); loc != nil {
		bad := (*body)[loc[1]:]
		if end := strings.Index(bad, ")"); end >= 0 {
			bad = bad[:end]
		}

		return fmt.Errorf("attach:%s: %w", bad, errBadAttach)
	}

	return nil
}

// upload sends one file once, remembering the URL a second ref to it reuses.
func (e *expander) upload(ctx context.Context, path string) (string, error) {
	full, err := resolveAttach(e.root, path)
	if err != nil {
		return "", err
	}

	if href, ok := e.seen[full]; ok {
		return href, nil
	}

	kind, ok := attachable[strings.ToLower(filepath.Ext(full))]
	if !ok {
		return "", fmt.Errorf("%s: %w", path, errNotAttachable)
	}

	data, err := readWithin(full, kind.limit)
	if err != nil {
		return "", fmt.Errorf("attach:%s: %w", path, err)
	}

	href, err := e.p.Upload(ctx, e.host, e.repoID, filepath.Base(full), kind.mime, data)
	if err != nil {
		return "", fmt.Errorf("uploading %s: %w", path, err)
	}

	e.seen[full] = href

	return href, nil
}

// resolveAttach turns the path a body names into the file it reads: ~ is the
// author's home, an absolute path is itself, and anything else is under the
// review's root, which is the checkout or state directory the artifact lives
// beside.
func resolveAttach(root, path string) (string, error) {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", path, err)
		}

		return filepath.Join(home, strings.TrimPrefix(path, "~/")), nil
	}
	if filepath.IsAbs(path) {
		return path, nil
	}

	return filepath.Join(root, path), nil
}

// readWithin reads the file or says which limit it broke, so a refusal names
// the number GitHub enforces rather than the server's own answer.
func readWithin(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("%s is %d bytes, %w (the cap is %d)",
			path, info.Size(), errAttachTooBig, limit)
	}

	data, err := os.ReadFile(path) //nolint:gosec // the path is the author's own attach: reference
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	return data, nil
}

// Upload posts one file to user-attachments and answers its URL. The endpoint
// is the same one the browser's drag-and-drop uses: it takes a bearer token
// where the API's own endpoints would, and answers the github.com asset URL a
// comment can embed and every reader can load.
func (ghPoster) Upload(
	ctx context.Context, host string, repoID int64, name, contentType string, data []byte,
) (string, error) {
	token, err := authToken(ctx, host)
	if err != nil {
		return "", err
	}

	return uploadAsset(ctx, uploadsHost(host), token, repoID, name, contentType, data)
}

// authToken asks gh for the token the upload endpoint wants, since the token
// lives in gh's keychain and nowhere else the process can read.
func authToken(ctx context.Context, host string) (string, error) {
	//nolint:gosec // host comes from the artifact's own Host field, not the network
	cmd := exec.CommandContext(ctx, "gh", "auth", "token", "--hostname", host)
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("gh auth token: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// uploadsHost is where the upload endpoint lives for the review's host.
// SECOND_LOOK_UPLOADS_BASE overrides it, which is how a test points the upload
// at a stub and how a GHE whose uploads host is spelled differently is still
// reachable.
func uploadsHost(host string) string {
	if base := os.Getenv("SECOND_LOOK_UPLOADS_BASE"); base != "" {
		return strings.TrimSuffix(base, "/")
	}

	return "https://uploads." + host
}

// uploadAsset is the POST itself, split from Upload so a test can stand it up
// against httptest without a token lookup.
func uploadAsset(
	ctx context.Context, base, token string, repoID int64, name, contentType string, data []byte,
) (string, error) {
	q := url.Values{
		keyName:         {name},
		"content_type":  {contentType},
		"repository_id": {strconv.FormatInt(repoID, 10)},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/user-attachments/assets?"+q.Encode(), bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("building the upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	// The endpoint 400s a body whose Content-Type is unset, which a reader body's is.
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("uploading: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // the answer is already read or already failed

	var answer struct {
		URL     string `json:"url"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return "", fmt.Errorf("reading the upload answer (status %d): %w", resp.StatusCode, err)
	}
	if answer.URL == "" {
		if answer.Message != "" {
			return "", fmt.Errorf("status %d (%s): %w", resp.StatusCode, answer.Message, errNoUploadURL)
		}

		return "", fmt.Errorf("status %d: %w", resp.StatusCode, errNoUploadURL)
	}

	return answer.URL, nil
}
