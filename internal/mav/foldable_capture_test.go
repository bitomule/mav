package mav

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const duoEnumerate = `Port:
    UUID: 289B9A50-63E7-4494-96AF-92AA1750D6C6
    Creatable Screen Properties:
    Connected Screens:
        Screen ID: 3
        Screen Type: Integrated
        Screen ID: 4
        Screen Type: CarPlay
        Screen ID: 5
        Screen Type: Scene
        Screen ID: 1
        Screen Type: Integrated
        Screen ID: 2
        Screen Type: TVOut
`

func TestIntegratedScreenIDsKeepsOnlyThePanels(t *testing.T) {
	runner := &sequenceRecordingRunner{out: map[string]string{"xcrun simctl io SIM enumerate": duoEnumerate}}
	got := integratedScreenIDs(context.Background(), runner, "SIM")
	if !reflect.DeepEqual(got, []string{"3", "1"}) {
		t.Fatalf("got %v, want the two integrated panels [3 1]", got)
	}
}

func writePNG(t *testing.T, path string, fill color.Color) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, fill)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// An open Duo's cover is a framebuffer of zeros; the panel showing the app is
// not. litFraction is what tells them apart.
func TestLitFractionTellsADarkPanelFromALitOne(t *testing.T) {
	dir := t.TempDir()
	dark := filepath.Join(dir, "dark.png")
	lit := filepath.Join(dir, "lit.png")
	writePNG(t, dark, color.Black)
	writePNG(t, lit, color.White)
	if got := litFraction(dark); got != 0 {
		t.Fatalf("dark panel read as %.2f lit", got)
	}
	if got := litFraction(lit); got < 0.99 {
		t.Fatalf("lit panel read as %.2f lit", got)
	}
}
