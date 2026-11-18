package launcher

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestLaunchVolumeFOVAndBobbing(t *testing.T) {
	for _, tc := range []struct {
		name, base, original, want string
		clientOptions              []string
		disableMusic               bool
	}{
		{
			name: "new profile with current native format", base: "26.3",
			clientOptions: []string{"soundCategory_\x01", "fov", "bobView"},
			want:          "soundCategory_master:0.30\nfov:0.375\nbobView:false\n",
		},
		{
			name: "future version still tries current keys", base: "35.1-snapshot-2",
			original: "resourcePacks:[\"file:p.zip\"]\nfov:0.1\nbobView:true\nsoundCategory_master:1.0\n",
			want:     "resourcePacks:[\"file:p.zip\"]\nfov:0.375\nbobView:false\nsoundCategory_master:0.30\n",
		},
		{
			name: "detected unsupported options stay untouched", base: "35.1",
			clientOptions: []string{"unrelatedOption", "fovEffectScale", "bobViewEnabled"},
			original:      "fov:0.1\nbobView:true\n",
			want:          "fov:0.1\nbobView:true\n",
		},
		{
			name: "older clients cap channels independently", base: "1.5.2",
			original: "sound:0.8\nmusic:0.2\nfov:0.0\nbobView:true\n",
			want:     "sound:0.30\nmusic:0.2\nfov:0.375\nbobView:false\n",
		},
		{
			name: "music toggle takes precedence before FOV existed", base: "b1.7.3",
			original: "sound:0.8\nmusic:1.0\n", disableMusic: true,
			want: "sound:0.30\nmusic:0.0\nbobView:false\n",
		},
		{
			name: "classic boolean audio has no intermediate volume", base: "c0.30_01c",
			clientOptions: []string{"options.txt", "music", "sound", "bobView"},
			original:      "sound:true\nmusic:false\n",
			want:          "sound:true\nmusic:false\nbobView:false\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			r := Receipt{Base: tc.base, GameDir: filepath.Join(root, "instance")}
			if tc.clientOptions != nil {
				var data bytes.Buffer
				client := zip.NewWriter(&data)
				entry, err := client.Create("net/minecraft/client/Options.class")
				if err != nil {
					t.Fatal(err)
				}
				for _, key := range tc.clientOptions {
					if _, err = entry.Write(append([]byte{1, 0, byte(len(key))}, key...)); err != nil {
						t.Fatal(err)
					}
				}
				if err = client.Close(); err != nil {
					t.Fatal(err)
				}
				if err = save(filepath.Join(root, "versions", tc.base, tc.base+".jar"), data.Bytes()); err != nil {
					t.Fatal(err)
				}
			}
			file := filepath.Join(r.GameDir, "options.txt")
			if tc.original != "" {
				if err := save(file, []byte(tc.original)); err != nil {
					t.Fatal(err)
				}
			}
			prefs := GamePreferences{MasterVolume: "30", FOV: "85", DisableBobbing: true, DisableMusic: tc.disableMusic}
			if err := New(root).applyGamePreferences(r, prefs); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(file)
			if err != nil || string(got) != tc.want {
				t.Fatalf("options.txt = %q (%v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestApplyGamePreferencesToInstance(t *testing.T) {
	for _, tc := range []struct {
		name, base, localeAsset, original, want string
		inIndex                                 bool
	}{
		{
			name: "modpack on a future release", base: "30.1-snapshot-1",
			localeAsset: "minecraft/lang/ru_ru.json", inIndex: true,
			original: "version:5555\r\nkey_test:key.keyboard.r\r\nlang:en_us\r\nresourcePacks:[\"test:pack\"]\r\n",
			want:     "version:5555\r\nkey_test:key.keyboard.r\r\nlang:ru_ru\r\nresourcePacks:[\"test:pack\"]\r\nguiScale:4\r\nsoundCategory_music:0.0\r\nnarrator:0\r\nnarratorHotkey:false\r\n",
		},
		{
			name: "legacy locale casing in the client JAR", base: "1.5.2",
			localeAsset: "lang/ru_RU.lang",
			original:    "music:0.6\nsound:1.0\nlang:en_US",
			want:        "music:0.0\nsound:1.0\nlang:ru_RU\nguiScale:3\n",
		},
		{
			name: "existing locale retained when unavailable", base: "1.12.2",
			localeAsset: "assets/minecraft/lang/en_us.lang",
			original:    "lang:en_us\nnarrator:2\n",
			want:        "lang:en_us\nnarrator:0\nguiScale:3\nsoundCategory_music:0.0\n",
		},
		{
			name: "alpha only mutes music", base: "a1.2.6",
			original: "music:true\nsound:true",
			want:     "music:false\nsound:true",
		},
		{
			name: "fresh version creates only native keys", base: "1.21.1",
			localeAsset: "minecraft/lang/ru_ru.json", inIndex: true,
			want: "lang:ru_ru\nguiScale:4\nsoundCategory_music:0.0\nnarrator:0\nnarratorHotkey:false\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			s := New(root)
			r := Receipt{ID: "build-local", Base: tc.base, GameDir: filepath.Join(root, "instances", "build-local")}
			if tc.inIndex {
				r.AssetIndexID = "example"
				index := filepath.Join(root, "assets", "indexes", r.AssetIndexID+".json")
				if err := save(index, []byte(`{"objects":{"`+tc.localeAsset+`":{"hash":"abc","size":1}}}`)); err != nil {
					t.Fatal(err)
				}
			} else if tc.localeAsset != "" {
				dir := filepath.Join(root, "versions", r.Base)
				if err := os.MkdirAll(dir, 0755); err != nil {
					t.Fatal(err)
				}
				file, err := os.Create(filepath.Join(dir, r.Base+".jar"))
				if err != nil {
					t.Fatal(err)
				}
				jar := zip.NewWriter(file)
				if _, err := jar.Create(tc.localeAsset); err != nil {
					t.Fatal(err)
				}
				if err := jar.Close(); err != nil {
					t.Fatal(err)
				}
				if err := file.Close(); err != nil {
					t.Fatal(err)
				}
			}
			file := filepath.Join(r.GameDir, "options.txt")
			if tc.original != "" {
				if err := save(file, []byte(tc.original)); err != nil {
					t.Fatal(err)
				}
			}
			prefs := GamePreferences{Language: "ru_ru", GUIScale: "4", DisableMusic: true, DisableNarrator: true}
			for range 2 {
				if err := s.applyGamePreferences(r, prefs); err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(file)
				if err != nil || string(got) != tc.want {
					t.Fatalf("options.txt = %q (%v), want %q", got, err, tc.want)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "options.txt")); !os.IsNotExist(err) {
				t.Fatalf("global game directory was touched: %v", err)
			}
		})
	}
}

func TestGamePreferenceDefaultsAndSnapshots(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	r := Receipt{Base: "b1.4", GameDir: root}
	if err := s.applyGamePreferences(r, GamePreferences{Language: "ru_ru", GUIScale: "2", DisableNarrator: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "options.txt")); !os.IsNotExist(err) {
		t.Fatalf("unsupported options created a file: %v", err)
	}
	if got := DefaultSettings(root).LaunchOptions(8192).GamePreferences; got != (GamePreferences{}) {
		t.Fatalf("game options should be opt-in, got %+v", got)
	}
	if optionFormat("13w35b").modernMusic || !optionFormat("13w36a").modernMusic ||
		optionFormat("17w12a").narrator || !optionFormat("17w13a").narrator ||
		optionFormat("23w32a").narratorHotkey || !optionFormat("23w33a").narratorHotkey {
		t.Fatal("incorrect snapshot option boundaries")
	}
}
