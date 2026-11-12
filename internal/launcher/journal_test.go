package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJournalKeepsStartupEvidenceAndTail(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".sintet", "logs")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	evidence := `<log4j:Event logger="net.minecraftforge.eventbus.EventSubclassTransformer" level="ERROR" thread="Sintet game bridge">`
	text := "Sintet | build-jzz-62b51da1b2 | 2026-11-10T15:53:45+02:00\r\n" + evidence + "\r\n" +
		strings.Repeat(strings.Repeat("x", 1000)+"\n", 1100) + "Minecraft завершился с ошибкой: exit status 0xffffffff\n"
	if err := os.WriteFile(filepath.Join(dir, "build-jzz-62b51da1b2-20261110-155345.log"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	s := New(root)
	got, err := s.Journal("")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Partial || got.Profile != "build-jzz-62b51da1b2" || !strings.Contains(got.Text, "exit status 0xffffffff") {
		t.Fatalf("journal lost context: partial=%t profile=%q", got.Partial, got.Profile)
	}
	if len(got.Findings) != 1 || got.Findings[0].ID != "bridge-forge" || got.Findings[0].Line != 2 || got.Findings[0].Evidence != evidence {
		t.Fatalf("incorrect diagnosis or evidence: %+v", got.Findings)
	}
	unchanged, err := s.Journal(got.Revision)
	if err != nil || unchanged.Revision != got.Revision || unchanged.Text != "" {
		t.Fatal("unchanged journal was sent again", err)
	}
}

func TestJournalSeparatesKnownFailuresFromGenericExitCodes(t *testing.T) {
	for _, tc := range []struct{ line, id string }{
		{"для выбранной версии нужна Java 21, а выбрана Java 17", "java-version"},
		{"java.lang.OutOfMemoryError: Java heap space", "heap-memory"},
		{"Mod 'Example' requires any version of base, which is missing!", "mod-dependency"},
		{"C  [sintet-overlay.dll+0x100]", "overlay-native"},
		{"Minecraft завершился с ошибкой: exit status 0xffffffff", "unknown"},
		{"[WARN] The mod recommends an optional library", ""},
	} {
		got := analyseJournal(tc.line)
		if tc.id == "" {
			if len(got) != 0 {
				t.Errorf("warning diagnosed as a crash: %+v", got)
			}
		} else if len(got) != 1 || got[0].ID != tc.id {
			t.Errorf("%q: got %+v, want %s", tc.line, got, tc.id)
		}
	}
}
