package mav

import (
	"bufio"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
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

	// Tree snapshot fields (P4 upgrade). Populated when the step also
	// persisted an accessibility tree under <runDir>/trees/. Absent on
	// older evidence steps; readers MUST tolerate empty values.
	TreePath  string `json:"tree_path,omitempty"`
	FullPath  string `json:"tree_full_path,omitempty"`
	DeltaPath string `json:"tree_delta_path,omitempty"`
	TreeHash  string `json:"tree_hash,omitempty"`

	// Video correlation (P4 upgrade). When a recording is active,
	// MonotonicMs is the host monotonic clock at capture; VideoOffsetMs
	// is monotonic_ms minus the recording's start, in milliseconds.
	// Both empty when no recording is running.
	MonotonicMs   int64 `json:"monotonic_ms,omitempty"`
	VideoOffsetMs int64 `json:"video_offset_ms,omitempty"`
}

type ReportData struct {
	RunID     string `json:"run_id"`
	CreatedAt string `json:"created_at"`
	// Fixture es el estado con nombre que se sembro antes de lanzar la app, si
	// hubo alguno. Sin esto el manifiesto no puede responder "de que estado
	// partio esto?", y un run cuya evidencia no dice eso no es reproducible --
	// que es justo lo que el manifiesto verificado promete.
	Fixture            string               `json:"fixture,omitempty"`
	Dir                string               `json:"dir"`
	Screenshot         string               `json:"screenshot,omitempty"`
	ScreenshotEvidence ImageEvidence        `json:"screenshot_evidence"`
	Steps              []ReportEvidenceStep `json:"steps"`
	Video              string               `json:"video,omitempty"`
	VideoMP4           string               `json:"video_mp4,omitempty"`
	VideoStatus        string               `json:"video_status"`
	VideoIssue         string               `json:"video_issue,omitempty"`
	VideoDuration      string               `json:"video_duration,omitempty"`
	VideoFrames        string               `json:"video_frames,omitempty"`
	Logs               string               `json:"logs,omitempty"`
	Network            NetworkEvidence      `json:"network,omitempty"`
	Crashes            []string             `json:"crashes,omitempty"`
	Commands           []string             `json:"commands,omitempty"`
	Issues             []ReportIssue        `json:"issues,omitempty"`
	ValidStepCount     int                  `json:"valid_step_count"`
	InvalidStepCount   int                  `json:"invalid_step_count"`
	Verdict            string               `json:"verdict"`
	Outputs            map[string]string    `json:"outputs,omitempty"`
}

type ReportEvidenceStep struct {
	EvidenceStep
	Index       int           `json:"index"`
	DisplayName string        `json:"display_name"`
	Image       ImageEvidence `json:"image"`
}

type ImageEvidence struct {
	OK     bool   `json:"ok"`
	Issue  string `json:"issue,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

type NetworkEvidence struct {
	HAR           string `json:"har,omitempty"`
	OK            bool   `json:"ok"`
	Issue         string `json:"issue,omitempty"`
	Requests      int    `json:"requests,omitempty"`
	Responses     int    `json:"responses,omitempty"`
	Status4xx     int    `json:"status_4xx,omitempty"`
	Status5xx     int    `json:"status_5xx,omitempty"`
	UniqueDomains int    `json:"unique_domains,omitempty"`
	Active        bool   `json:"active,omitempty"`
}

type ReportIssue struct {
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

func GenerateReport(run RunState) (string, error) {
	data := ReportData{
		RunID:     run.ID,
		CreatedAt: time.Now().Format(time.RFC3339),
		Dir:       run.Dir,
		Fixture:   readRunFixture(run),
	}
	for index, step := range LoadEvidenceSteps(run) {
		reportStep := ReportEvidenceStep{
			EvidenceStep: step,
			Index:        index + 1,
			DisplayName:  humanizeEvidenceName(step.Name),
			Image:        ValidateEvidenceImage(step.File),
		}
		if reportStep.Image.OK {
			data.ValidStepCount++
		} else {
			data.InvalidStepCount++
			data.Issues = append(data.Issues, ReportIssue{
				Severity: "blocker",
				Title:    "Evidence image is not usable",
				Detail:   fmt.Sprintf("%s: %s", step.Name, reportStep.Image.Issue),
			})
		}
		if strings.TrimSpace(step.Note) == "" {
			data.Issues = append(data.Issues, ReportIssue{
				Severity: "warning",
				Title:    "Evidence step has no assertion note",
				Detail:   fmt.Sprintf("%s should explain what the screenshot proves.", step.Name),
			})
		}
		data.Steps = append(data.Steps, reportStep)
	}
	if len(data.Steps) == 0 {
		data.Issues = append(data.Issues, ReportIssue{
			Severity: "warning",
			Title:    "No named evidence steps",
			Detail:   "Capture named before/after or state-specific proof points with mav evidence step.",
		})
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
	if data.Screenshot != "" {
		data.ScreenshotEvidence = ValidateEvidenceImage(data.Screenshot)
		if !data.ScreenshotEvidence.OK {
			data.Issues = append(data.Issues, ReportIssue{
				Severity: "warning",
				Title:    "Current screenshot is not usable",
				Detail:   data.ScreenshotEvidence.Issue,
			})
		}
	}
	if video, validation := reportVideo(run); video != "" {
		data.Video = video
		if mp4 := reportVideoMP4(run); mp4 != "" {
			data.VideoMP4 = mp4
		}
		if validation.OK {
			data.VideoStatus = "accepted"
			data.VideoDuration = validation.Duration.String()
			if validation.Frames > 0 {
				data.VideoFrames = fmt.Sprintf("%d", validation.Frames)
			} else {
				data.VideoFrames = "unknown"
			}
		} else {
			data.VideoStatus = "invalid"
			data.VideoIssue = validation.Issue
			if validation.Duration > 0 {
				data.VideoDuration = validation.Duration.String()
			} else {
				data.VideoDuration = "0s"
			}
			if validation.Frames > 0 {
				data.VideoFrames = fmt.Sprintf("%d", validation.Frames)
			} else {
				data.VideoFrames = "unknown"
			}
			data.Issues = append(data.Issues, ReportIssue{
				Severity: "blocker",
				Title:    "Video is not accepted as evidence",
				Detail:   fmt.Sprintf("%s (%s)", validation.Issue, data.VideoDuration),
			})
		}
	} else {
		data.VideoStatus = "missing"
		data.Issues = append(data.Issues, ReportIssue{
			Severity: "warning",
			Title:    "No video evidence captured",
			Detail:   "Screenshot evidence is still shown, but the report does not prove the interaction sequence.",
		})
	}
	if exists(run.LogsPath) {
		content, _ := os.ReadFile(run.LogsPath)
		lines := strings.Split(string(content), "\n")
		if len(lines) > 120 {
			lines = lines[len(lines)-120:]
		}
		data.Logs = strings.Join(lines, "\n")
	}
	if network := reportNetwork(run); network.HAR != "" || network.Active {
		data.Network = network
		if network.Issue != "" {
			data.Issues = append(data.Issues, ReportIssue{
				Severity: "warning",
				Title:    "Network capture needs review",
				Detail:   network.Issue,
			})
		}
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
	if outputData, err := os.ReadFile(filepath.Join(run.Dir, "outputs.json")); err == nil {
		_ = json.Unmarshal(outputData, &data.Outputs)
	}
	data.Verdict = "needs review"
	if data.InvalidStepCount == 0 && data.VideoStatus == "accepted" && len(data.Steps) > 0 {
		data.Verdict = "verified"
	}
	if data.InvalidStepCount > 0 || data.VideoStatus == "invalid" {
		data.Verdict = "blocked"
	}
	path := filepath.Join(run.Dir, "report.json")
	file, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return path, encoder.Encode(data)
}

func reportNetwork(run RunState) NetworkEvidence {
	har := filepath.Join(run.Dir, "network.har")
	evidence := NetworkEvidence{HAR: har, Active: findRunningNetworkPID(run) > 0}
	info, err := os.Stat(har)
	if err != nil {
		if evidence.Active {
			evidence.Issue = "network_capture_active_no_har"
		}
		evidence.HAR = ""
		return evidence
	}
	if info.Size() == 0 {
		evidence.Issue = "har_empty"
		return evidence
	}
	summary := summarizeHAR(har)
	if summary["har_parse"] == "failed" {
		evidence.Issue = "har_parse_failed"
		return evidence
	}
	evidence.OK = true
	evidence.Requests, _ = strconv.Atoi(summary["requests"])
	evidence.Responses, _ = strconv.Atoi(summary["responses"])
	evidence.Status4xx, _ = strconv.Atoi(summary["status_4xx"])
	evidence.Status5xx, _ = strconv.Atoi(summary["status_5xx"])
	evidence.UniqueDomains, _ = strconv.Atoi(summary["unique_domains"])
	if evidence.Active {
		evidence.Issue = "network_capture_still_active"
	}
	return evidence
}

func ValidateEvidenceImage(path string) ImageEvidence {
	info, err := os.Stat(path)
	if err != nil {
		return ImageEvidence{Issue: "file_missing"}
	}
	if info.Size() == 0 {
		return ImageEvidence{Size: info.Size(), Issue: "file_empty"}
	}
	file, err := os.Open(path)
	if err != nil {
		return ImageEvidence{Size: info.Size(), Issue: "file_unreadable"}
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return ImageEvidence{Size: info.Size(), Issue: "image_decode_failed"}
	}
	evidence := ImageEvidence{OK: true, Width: config.Width, Height: config.Height, Size: info.Size()}
	if config.Width <= 0 || config.Height <= 0 {
		evidence.OK = false
		evidence.Issue = "image_dimensions_missing"
	} else if config.Width < 24 || config.Height < 24 {
		evidence.OK = false
		evidence.Issue = "image_dimensions_too_small"
	}
	return evidence
}

func humanizeEvidenceName(name string) string {
	name = strings.TrimSpace(strings.ReplaceAll(name, "-", " "))
	if name == "" {
		return "Evidence step"
	}
	parts := strings.Fields(name)
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}

func reportVideo(run RunState) (string, VideoValidation) {
	for _, name := range []string{"video.mov", "video.mp4"} {
		path := filepath.Join(run.Dir, name)
		if exists(path) {
			return path, ValidateEvidenceVideo(path)
		}
	}
	return "", VideoValidation{}
}

func reportVideoMP4(run RunState) string {
	path := filepath.Join(run.Dir, "video.mp4")
	if exists(path) {
		return path
	}
	return ""
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

// runFixturePath es donde `mav open` deja constancia del fixture aplicado, para
// que el manifiesto de evidencia pueda decir de que estado partio el run.
func runFixturePath(run RunState) string {
	return filepath.Join(run.Dir, "fixture.txt")
}

func writeRunFixture(run RunState, fixture string) {
	if fixture == "" {
		return
	}
	_ = os.WriteFile(runFixturePath(run), []byte(fixture+"\n"), 0o644)
}

func readRunFixture(run RunState) string {
	data, err := os.ReadFile(runFixturePath(run))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
