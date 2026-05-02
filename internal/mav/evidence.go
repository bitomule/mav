package mav

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ReportData struct {
	RunID           string
	CreatedAt       string
	Dir             string
	Screenshot      string
	StepScreenshots []string
	Video           string
	Logs            string
	Crashes         []string
}

func GenerateReport(run RunState) (string, error) {
	data := ReportData{
		RunID:     run.ID,
		CreatedAt: time.Now().Format(time.RFC3339),
		Dir:       run.Dir,
	}
	screen := filepath.Join(run.Dir, "screen.png")
	if exists(screen) {
		data.Screenshot = screen
	}
	if matches, err := filepath.Glob(filepath.Join(run.Dir, "maestro", "mav_*.png")); err == nil {
		data.StepScreenshots = matches
	}
	for _, name := range []string{"video.mp4", "video.mov"} {
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
	path := filepath.Join(run.Dir, "report.html")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	return path, reportTemplate.Execute(file, data)
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
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 32px; color: #171717; background: #fafafa; }
    main { max-width: 980px; margin: 0 auto; }
    h1 { font-size: 24px; margin-bottom: 4px; }
    h2 { font-size: 16px; margin-top: 28px; }
    code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
    pre { white-space: pre-wrap; background: #111; color: #f7f7f7; padding: 16px; border-radius: 8px; overflow: auto; }
    img, video { max-width: 100%; border: 1px solid #ddd; border-radius: 8px; background: white; }
    .meta { color: #666; font-size: 13px; }
    .empty { color: #777; }
  </style>
</head>
<body>
<main>
  <h1>MAV Evidence</h1>
  <div class="meta">run={{.RunID}} created={{.CreatedAt}} dir={{.Dir}}</div>
  <h2>Screenshot</h2>
  {{if .Screenshot}}<img src="{{.Screenshot}}" alt="MAV screenshot">{{else}}<p class="empty">No screenshot captured.</p>{{end}}
  <h2>Step Screenshots</h2>
  {{if .StepScreenshots}}{{range .StepScreenshots}}<p><img src="{{.}}" alt="MAV step screenshot"></p>{{end}}{{else}}<p class="empty">No step screenshots captured.</p>{{end}}
  <h2>Video</h2>
  {{if .Video}}<video src="{{.Video}}" controls></video>{{else}}<p class="empty">No video captured.</p>{{end}}
  <h2>Logs</h2>
  {{if .Logs}}<pre>{{.Logs}}</pre>{{else}}<p class="empty">No logs captured.</p>{{end}}
  <h2>Crashes</h2>
  {{if .Crashes}}{{range .Crashes}}<p><code>{{.}}</code></p>{{end}}{{else}}<p class="empty">No crashes captured.</p>{{end}}
</main>
</body>
</html>
`))
