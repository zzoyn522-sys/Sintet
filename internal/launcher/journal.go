package launcher

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type GameJournal struct {
	Revision string           `json:"revision"`
	Profile  string           `json:"profile"`
	Text     string           `json:"text"`
	Partial  bool             `json:"partial"`
	Findings []JournalFinding `json:"findings"`
}

type JournalFinding struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Advice   string `json:"advice"`
	Evidence string `json:"evidence"`
	Line     int    `json:"line"`
}

type journalRule struct {
	id, title, advice string
	match             *regexp.Regexp
}

// Local signatures follow the problem/evidence/solution approach of Aternos Codex.
// Keep specific causes ahead of downstream exceptions.
var journalRules = []journalRule{
	{"bridge-forge", "Конфликт Java-моста Sintet и Forge", "Проверьте, установлена ли последняя версия Sintet. Если сбой повторится, приложите эту строку и версию Forge к сообщению об ошибке.", regexp.MustCompile(`(?i)EventSubclassTransformer[^\n]{0,200}level="ERROR"[^\n]{0,100}Sintet game bridge|Could not find parent [^\n]{0,600}on thread Thread\[Sintet game bridge`)},
	{"overlay-native", "Сбой нативного оверлея Sintet", "Проверьте наличие обновления Sintet. Если сбой повторится, приложите отмеченную строку к сообщению об ошибке.", regexp.MustCompile(`(?i)^\s*#?\s*[CV]\s+\[sintet-overlay\.dll\+|(?:could not find agent library|agent library failed to init)[^\n]{0,300}sintet-overlay\.dll`)},
	{"bridge-agent", "Не удалось загрузить Java-мост Sintet", "Проверьте наличие обновления Sintet. Если мост снова не загрузится, приложите отмеченную строку к сообщению об ошибке.", regexp.MustCompile(`(?i)(?:error opening zip file|jar manifest missing|failed to find premain-class)[^\n]{0,360}sintet-bridge\.jar`)},
	{"java-version", "Версия Java не подходит", "Откройте настройки сборки и выберите Java, рекомендованную для этой версии Minecraft.", regexp.MustCompile(`(?i)UnsupportedClassVersionError|class file version [0-9.]+.*only recognizes|(?:требуется|нужна) Java \d+|requires Java \d+`)},
	{"heap-memory", "Minecraft не хватило оперативной памяти", "Закройте тяжёлые программы или немного увеличьте максимум ОЗУ в настройках сборки.", regexp.MustCompile(`(?i)OutOfMemoryError: (?:Java heap space|GC overhead limit exceeded)|java\.lang\.OutOfMemoryError: Requested array size`)},
	{"jvm-reserve", "Java не смогла выделить память", "Проверьте, что используется 64-битная Java, уменьшите максимальную память или закройте другие программы.", regexp.MustCompile(`(?i)Could not reserve enough space|There is insufficient memory for the Java Runtime Environment|Native memory allocation.*failed|Invalid maximum heap size`)},
	{"disk-space", "На диске закончилось место", "Освободите место на диске, где лежат игра и сборка, затем повторите запуск.", regexp.MustCompile(`(?i)No space left on device|not enough space on the disk|Недостаточно места на диске`)},
	{"game-files", "Повреждены или отсутствуют файлы игры", "Проверьте и переустановите версию Minecraft из журнала. Если указан мод, переустановите именно его.", regexp.MustCompile(`(?i)повреждён или отсутствует файл|invalid (?:CEN|LOC) header|zip END header not found`)},
	{"mod-dependency", "Моду не хватает зависимости", "Откройте указанную строку и установите требуемый мод для этой версии Minecraft и загрузчика.", regexp.MustCompile(`(?i)Missing (?:or unsupported )?mandatory dependencies|requires (?:any version|version [^\n]{1,100}) of [^\n]{1,180}(?:which is missing|not installed)|requires [^\n]{1,180} which is (?:missing|not installed)`)},
	{"mod-duplicate", "Загружаются две копии одного мода", "В списке модов сборки оставьте одну совместимую версию указанного мода.", regexp.MustCompile(`(?i)Found duplicate mods|Duplicate Mods Found|duplicate mod(?: ID|s:)|DuplicateModsFoundException`)},
	{"mod-mismatch", "Загрузчик нашёл несовместимый мод", "Проверьте, что указанный мод подходит для версии игры и загрузчика этой сборки.", regexp.MustCompile(`(?i)Incompatible mods found|Mod resolution encountered an incompatible mod set|is not a valid mod file|is for a different (?:version of Minecraft|mod loader)`)},
	{"mixin", "Мод не смог применить Mixin", "Посмотрите имя мода или файла mixin в журнале. Проверьте его версию или временно отключите этот мод для проверки.", regexp.MustCompile(`(?i)Mixin apply failed|MixinApplyError|Critical injection failure|InjectionError: Critical`)},
	{"graphics", "Minecraft не смог запустить графику", "Проверьте драйвер видеокарты и поддержку OpenGL. Попробуйте запустить игру без сторонних графических модов.", regexp.MustCompile(`(?i)GLFW error 6554[1235]|Failed to create (?:the )?GLFW window|The driver does not appear to support OpenGL|Pixel format not accelerated`)},
}

func analyseJournal(text string) []JournalFinding {
	findings := []JournalFinding{}
	lines := strings.Split(text, "\n")
	for _, rule := range journalRules {
		for index, line := range lines {
			if rule.match.MatchString(line) {
				findings = append(findings, JournalFinding{rule.id, rule.title, rule.advice, journalEvidence(line), index + 1})
				break
			}
		}
		if len(findings) == 5 {
			break
		}
	}
	if len(findings) == 0 {
		for i, line := range lines {
			if strings.Contains(line, "Minecraft завершился с ошибкой:") || strings.HasPrefix(line, "Ошибка запуска:") || strings.Contains(line, "Game crashed! Crash report saved to:") {
				findings = append(findings, JournalFinding{"unknown", "Причина сбоя не распознана", "Посмотрите первые ошибки и блок Caused by во вкладке «Лог».", journalEvidence(line), i + 1})
				break
			}
		}
	}
	return findings
}

func journalEvidence(line string) string {
	line = strings.TrimSpace(line)
	if len(line) > 440 {
		line = strings.ToValidUTF8(line[:440], "") + "…"
	}
	return line
}

func (s *Service) Journal(revision string) (GameJournal, error) {
	state := s.State()
	if state.Error != "" && !state.Running && state.Log == "" {
		text := state.Stage + "\n" + state.Error
		if state.ErrorDetails != "" && state.ErrorDetails != state.Error {
			text += "\n\nТехнические подробности:\n" + state.ErrorDetails
		}
		stamp := fmt.Sprintf("operation:%x", sha256.Sum256([]byte(text)))
		result := GameJournal{Revision: stamp, Profile: state.Profile, Findings: []JournalFinding{}}
		if revision != stamp {
			result.Text = text
			result.Findings = analyseJournal(text)
		}
		return result, nil
	}
	path := s.currentLogPath()
	if path == "" {
		return GameJournal{Findings: []JournalFinding{}}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return GameJournal{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return GameJournal{}, err
	}
	if !info.Mode().IsRegular() {
		return GameJournal{}, fmt.Errorf("журнал не является обычным файлом")
	}
	stamp := fmt.Sprintf("%s:%d:%d", filepath.Base(path), info.Size(), info.ModTime().UnixNano())
	result := GameJournal{Revision: stamp}
	if revision == stamp {
		return result, nil
	}

	const headLimit = 256 << 10
	const tailLimit = 768 << 10
	if info.Size() <= headLimit+tailLimit {
		data, err := io.ReadAll(io.LimitReader(f, info.Size()))
		if err != nil {
			return GameJournal{}, err
		}
		result.Text = string(data)
	} else {
		head, err := io.ReadAll(io.LimitReader(f, headLimit))
		if err != nil {
			return GameJournal{}, err
		}
		tail, err := io.ReadAll(io.NewSectionReader(f, info.Size()-tailLimit, tailLimit))
		if err != nil {
			return GameJournal{}, err
		}
		headText, tailText := string(head), string(tail)
		if i := strings.LastIndexByte(headText, '\n'); i >= 0 {
			headText = headText[:i]
		}
		if i := strings.IndexByte(tailText, '\n'); i >= 0 {
			tailText = tailText[i+1:]
		}
		result.Text = headText + "\n[…] Пропущена середина длинного журнала […]\n" + tailText
		result.Partial = true
	}
	result.Text = strings.ToValidUTF8(strings.ReplaceAll(result.Text, "\r\n", "\n"), "�")
	header, _, _ := strings.Cut(result.Text, "\n")
	if line, found := strings.CutPrefix(header, "Sintet | "); found {
		id, _, _ := strings.Cut(line, " | ")
		if validateID(id) == nil {
			result.Profile = id
		}
	}
	result.Findings = analyseJournal(result.Text)
	return result, nil
}
