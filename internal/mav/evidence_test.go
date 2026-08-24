package mav

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateReport(t *testing.T) {
	dir := t.TempDir()
	run := RunState{ID: "abc", Dir: dir, LogsPath: filepath.Join(dir, "logs.txt")}
	if err := os.WriteFile(run.LogsPath, []byte("hello log\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeTestPNG(filepath.Join(dir, "screen.png")); err != nil {
		t.Fatal(err)
	}
	stepFile := filepath.Join(dir, "steps", "01_notifications-before.png")
	if err := os.MkdirAll(filepath.Dir(stepFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeTestPNG(stepFile); err != nil {
		t.Fatal(err)
	}
	if err := AppendEvidenceStep(run, EvidenceStep{Name: "notifications-before", Note: "before toggling notifications", File: stepFile}); err != nil {
		t.Fatal(err)
	}
	maestroDir := filepath.Join(dir, "maestro")
	if err := os.MkdirAll(maestroDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(maestroDir, "mav_step_00_launch.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := GenerateReport(run)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report ReportData
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("invalid report json: %v\n%s", err, data)
	}
	if filepath.Base(path) != "report.json" {
		t.Fatalf("report path=%s", path)
	}
	if report.Logs != "hello log\n" || report.ScreenshotEvidence == nil || report.ScreenshotEvidence.Width != 32 || report.ValidStepCount != 1 {
		t.Fatalf("unexpected report data: %+v", report)
	}
	for _, want := range []string{"notifications-before", "before toggling notifications"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("report missing %q:\n%s", want, data)
		}
	}
	if strings.Contains(string(data), "maestro") || strings.Contains(string(data), "mav_step_00_launch") {
		t.Fatalf("report should ignore maestro artifacts:\n%s", data)
	}
}

func TestGenerateReportMarksTooShortVideoInvalid(t *testing.T) {
	dir := t.TempDir()
	run := RunState{ID: "abc", Dir: dir, LogsPath: filepath.Join(dir, "logs.txt")}
	if err := os.WriteFile(filepath.Join(dir, "video.mov"), testMovieWithDuration(600, 40), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := GenerateReport(run)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reportJSON := string(data)
	if !strings.Contains(reportJSON, `"video_status": "invalid"`) || !strings.Contains(reportJSON, "duration_too_short") {
		t.Fatalf("report should flag invalid video:\n%s", reportJSON)
	}
}

func TestGenerateReportIncludesEmbeddableMP4(t *testing.T) {
	dir := t.TempDir()
	run := RunState{ID: "abc", Dir: dir}
	if err := os.WriteFile(filepath.Join(dir, "video.mov"), testMovieWithDuration(600, 1200), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "video.mp4"), testMovieWithDuration(600, 1200), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := GenerateReport(run)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report ReportData
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("invalid report json: %v\n%s", err, data)
	}
	if filepath.Base(report.Video) != "video.mov" {
		t.Fatalf("source video=%q", report.Video)
	}
	if filepath.Base(report.VideoMP4) != "video.mp4" {
		t.Fatalf("video_mp4=%q", report.VideoMP4)
	}
}

func TestGenerateReportIncludesNetworkEvidence(t *testing.T) {
	dir := t.TempDir()
	run := RunState{ID: "abc", Dir: dir}
	har := `{"log":{"entries":[{"request":{"url":"https://api.example.com/a"},"response":{"status":200}},{"request":{"url":"https://api.example.com/b"},"response":{"status":500}}]}}`
	if err := os.WriteFile(filepath.Join(dir, "network.har"), []byte(har), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := GenerateReport(run)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report ReportData
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("invalid report json: %v\n%s", err, data)
	}
	if !report.Network.OK || report.Network.Requests != 2 || report.Network.Status5xx != 1 || report.Network.UniqueDomains != 1 {
		t.Fatalf("network=%+v", report.Network)
	}
}

func TestValidateEvidenceImageRejectsInvalidImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fake.png")
	if err := os.WriteFile(path, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ValidateEvidenceImage(path)
	if got.OK || got.Issue != "image_decode_failed" {
		t.Fatalf("got %+v", got)
	}
}

func TestSafeFileName(t *testing.T) {
	if got := safeFileName("Notifications: After Toggle"); got != "notifications-after-toggle" {
		t.Fatalf("got %q", got)
	}
	if got := safeFileName("Selecciona una categoría / ¿Cuánto pesa?"); got != "selecciona-una-categoria-cuanto-pesa" {
		t.Fatalf("got %q", got)
	}
}

func TestLoadEvidenceSteps(t *testing.T) {
	dir := t.TempDir()
	run := RunState{ID: "abc", Dir: dir}
	if err := AppendEvidenceStep(run, EvidenceStep{Name: "one", File: filepath.Join(dir, "one.png")}); err != nil {
		t.Fatal(err)
	}
	steps := LoadEvidenceSteps(run)
	if len(steps) != 1 || steps[0].Name != "one" || steps[0].Kind != "screenshot" {
		t.Fatalf("steps=%+v", steps)
	}
}

func writeTestPNG(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: 24, G: 96, B: 160, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return png.Encode(file, img)
}

// TestReportRecordsTheFixtureApplied: un run cuyo estado lo sembro un fixture y
// cuyo manifiesto no dice cual no es reproducible desde su propia evidencia,
// que es justo lo que el manifiesto verificado promete.
func TestReportRecordsTheFixtureApplied(t *testing.T) {
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	writeRunFixture(run, "seeded-meetings")
	path, err := GenerateReport(run)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report ReportData
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Fixture != "seeded-meetings" {
		t.Fatalf("report.json debe registrar el fixture aplicado, got %q", report.Fixture)
	}
}

func TestReportOmitsFixtureWhenNoneApplied(t *testing.T) {
	run, err := NewRunState()
	if err != nil {
		t.Fatal(err)
	}
	path, err := GenerateReport(run)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\"fixture\"") {
		t.Fatalf("sin fixture no debe aparecer el campo:\n%s", data)
	}
}

// TestReportOmitsScreenshotVerdictWhenThereIsNoScreenshot: un flow no deja
// captura suelta, y el campo salia igualmente como {"ok":false}. Un veredicto
// negativo sobre algo que nunca existio es indistinguible de una captura rota
// para quien lee el JSON, que es justo lo que una capa de evidencia no puede
// permitirse.
func TestReportOmitsScreenshotVerdictWhenThereIsNoScreenshot(t *testing.T) {
	dir := t.TempDir()
	run := RunState{ID: "sin-captura", Dir: dir}
	path, err := GenerateReport(run)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var populated ReportData
	if err := json.Unmarshal(encoded, &populated); err != nil {
		t.Fatal(err)
	}
	if populated.ScreenshotEvidence != nil {
		t.Fatalf("sin captura no debe haber veredicto: %+v", populated.ScreenshotEvidence)
	}
	if strings.Contains(string(encoded), "screenshot_evidence") {
		t.Fatalf("el campo debe estar ausente, no en falso: %s", encoded)
	}
}
