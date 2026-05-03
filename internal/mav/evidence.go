package mav

import (
	"bufio"
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const EvidenceStepsFile = "evidence.jsonl"

type EvidenceStep struct {
	Name      string `json:"name"`
	Note      string `json:"note,omitempty"`
	File      string `json:"file"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
}

type ReportData struct {
	RunID      string
	CreatedAt  string
	Dir        string
	Screenshot string
	Steps      []EvidenceStep
	Video      string
	Logs       string
	Crashes    []string
	Commands   []string
}

func GenerateReport(run RunState) (string, error) {
	data := ReportData{
		RunID:     run.ID,
		CreatedAt: time.Now().Format(time.RFC3339),
		Dir:       run.Dir,
		Steps:     LoadEvidenceSteps(run),
	}
	screen := filepath.Join(run.Dir, "screen.png")
	if exists(screen) {
		data.Screenshot = screen
	}
	if latest, err := os.ReadFile(filepath.Join(run.Dir, "latest_capture.txt")); err == nil {
		path := strings.TrimSpace(string(latest))
		if path != "" && exists(path) {
			data.Screenshot = path
		}
	}
	for _, name := range []string{"video.mov", "video.mp4"} {
		path := filepath.Join(run.Dir, name)
		if exists(path) {
			data.Video = path
			break
		}
	}
	if exists(run.LogsPath) {
		content, _ := os.ReadFile(run.LogsPath)
		lines := strings.Split(string(content), "\n")
		if len(lines) > 120 {
			lines = lines[len(lines)-120:]
		}
		data.Logs = strings.Join(lines, "\n")
	}
	crashDir := filepath.Join(run.Dir, "crashes")
	if entries, err := os.ReadDir(crashDir); err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				data.Crashes = append(data.Crashes, filepath.Join(crashDir, entry.Name()))
			}
		}
	}
	if exists(run.Commands) {
		content, _ := os.ReadFile(run.Commands)
		for _, line := range strings.Split(string(content), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				data.Commands = append(data.Commands, line)
			}
		}
	}
	path := filepath.Join(run.Dir, "report.html")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return path, reportTemplate.Execute(file, data)
}

func AppendEvidenceStep(run RunState, step EvidenceStep) error {
	if step.CreatedAt == "" {
		step.CreatedAt = time.Now().Format(time.RFC3339)
	}
	if step.Kind == "" {
		step.Kind = "screenshot"
	}
	data, err := json.Marshal(step)
	if err != nil {
		return err
	}
	return appendLine(filepath.Join(run.Dir, EvidenceStepsFile), string(data))
}

func appendLine(path, line string) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(line + "\n")
	return err
}

func LoadEvidenceSteps(run RunState) []EvidenceStep {
	path := filepath.Join(run.Dir, EvidenceStepsFile)
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	var steps []EvidenceStep
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var step EvidenceStep
		if err := json.Unmarshal(scanner.Bytes(), &step); err == nil && step.File != "" {
			steps = append(steps, step)
		}
	}
	return steps
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

var reportTemplate = template.Must(template.New("report").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>MAV Evidence {{.RunID}}</title>
  <style>
    :root { color-scheme: light; --ink:#171717; --muted:#6b7280; --line:#e5e7eb; --panel:#fff; --bg:#f6f7f9; --accent:#3157d5; }
    * { box-sizing: border-box; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 0; color: var(--ink); background: var(--bg); }
    main { max-width: 1180px; margin: 0 auto; padding: 28px; }
    header { display: flex; justify-content: space-between; gap: 24px; align-items: flex-start; margin-bottom: 24px; }
    h1 { font-size: 28px; margin: 0 0 6px; letter-spacing: 0; }
    h2 { font-size: 15px; margin: 0 0 14px; text-transform: uppercase; letter-spacing: .08em; color: var(--muted); }
    h3 { font-size: 17px; margin: 0 0 6px; }
    code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
    pre { white-space: pre-wrap; background: #111827; color: #f9fafb; padding: 16px; border-radius: 8px; overflow: auto; max-height: 360px; font-size: 12px; line-height: 1.45; }
    section { background: var(--panel); border: 1px solid var(--line); border-radius: 8px; padding: 18px; margin-bottom: 18px; }
    img, video { display: block; width: 100%; max-width: 390px; max-height: 760px; object-fit: contain; border: 1px solid var(--line); border-radius: 8px; background: white; }
    video { max-width: 430px; }
    .meta { color: var(--muted); font-size: 13px; line-height: 1.5; }
    .badge { display:inline-flex; align-items:center; border:1px solid var(--line); border-radius:999px; padding:6px 10px; background:white; color:var(--muted); font-size:12px; white-space: nowrap; }
    .grid { display:grid; grid-template-columns: 1fr 1fr; gap:18px; align-items:start; }
    .timeline { display:grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap:16px; }
    .step { border:1px solid var(--line); border-radius:8px; padding:12px; background:#fbfbfc; }
    .step img { max-width: 100%; max-height: 520px; margin-top: 10px; }
    .empty { color: var(--muted); margin: 0; }
    .commands { display:grid; gap:8px; }
    .command { background:#f3f4f6; border:1px solid var(--line); border-radius:6px; padding:8px; font-size:12px; overflow:auto; }
    @media (max-width: 820px) { main { padding: 16px; } header, .grid { display:block; } section { padding: 14px; } img, video { max-width: 100%; } }
  </style>
</head>
<body>
<main>
  <header>
    <div>
      <h1>MAV Evidence</h1>
      <div class="meta">run={{.RunID}}<br>created={{.CreatedAt}}<br>{{.Dir}}</div>
    </div>
    <div class="badge">{{if .Video}}video{{else}}no video{{end}} · {{len .Steps}} steps · {{if .Crashes}}{{len .Crashes}} crashes{{else}}0 crashes{{end}}</div>
  </header>

  <section>
    <h2>Flow Recording</h2>
    {{if .Video}}<video src="{{.Video}}" controls></video>{{else}}<p class="empty">No video captured. Start evidence recording before the flow and stop it after the tested behavior.</p>{{end}}
  </section>

  <section>
    <h2>Verification Timeline</h2>
    {{if .Steps}}
    <div class="timeline">
      {{range .Steps}}
      <article class="step">
        <h3>{{.Name}}</h3>
        <div class="meta">{{.Kind}} · {{.CreatedAt}}{{if .Note}}<br>{{.Note}}{{end}}</div>
        <img src="{{.File}}" alt="{{.Name}}">
      </article>
      {{end}}
    </div>
    {{else}}<p class="empty">No named evidence steps captured.</p>{{end}}
  </section>

  <div class="grid">
    <section>
      <h2>Current Screenshot</h2>
      {{if .Screenshot}}<img src="{{.Screenshot}}" alt="MAV screenshot">{{else}}<p class="empty">No current screenshot captured.</p>{{end}}
    </section>
    <section>
      <h2>Crashes</h2>
      {{if .Crashes}}{{range .Crashes}}<p><code>{{.}}</code></p>{{end}}{{else}}<p class="empty">No crashes captured.</p>{{end}}
    </section>
  </div>

  <section>
    <h2>Commands</h2>
    {{if .Commands}}<div class="commands">{{range .Commands}}<div class="command"><code>{{.}}</code></div>{{end}}</div>{{else}}<p class="empty">No commands recorded.</p>{{end}}
  </section>

  <section>
    <h2>Logs</h2>
    {{if .Logs}}<pre>{{.Logs}}</pre>{{else}}<p class="empty">No logs captured.</p>{{end}}
  </section>
</main>
</body>
</html>
`))
