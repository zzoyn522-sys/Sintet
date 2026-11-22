package sldcfg

import (
	"fmt"
	"regexp"
)

var backgroundFile = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|jpeg|webp|gif|mp4|webm)$`)
var appearanceFontFile = regexp.MustCompile(`^[0-9a-f]{32}\.(ttf|otf|woff|woff2)$`)
var themeColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

type OverlayAppearance struct {
	Position          string `json:"position"`
	Material          string `json:"material"`
	Surface           string `json:"surface"`
	Text              string `json:"text"`
	Accent            string `json:"accent"`
	Opacity           int    `json:"opacity"`
	Blur              int    `json:"blur"`
	Dim               int    `json:"dim"`
	Scale             int    `json:"scale"`
	Width             int    `json:"width"`
	Rounding          int    `json:"rounding"`
	UseLauncherAccent bool   `json:"useLauncherAccent"`
}

func DefaultOverlay() OverlayAppearance {
	return OverlayAppearance{Position: "top", Material: "acrylic", Surface: "#121214", Text: "#ededed", Accent: "#8ec5a2", Opacity: 75, Blur: 8, Dim: 40}
}

func (p OverlayAppearance) Valid() bool {
	return (p.Position == "top" || p.Position == "right" || p.Position == "bottom") &&
		(p.Material == "acrylic" || p.Material == "tint" || p.Material == "solid") &&
		themeColor.MatchString(p.Surface) && themeColor.MatchString(p.Text) && themeColor.MatchString(p.Accent) &&
		p.Opacity >= 0 && p.Opacity <= 100 && p.Blur >= 0 && p.Blur <= 24 && p.Dim >= 0 && p.Dim <= 100 &&
		(p.Scale == 0 || p.Scale >= 75 && p.Scale <= 125) &&
		(p.Width == 0 || p.Width >= 350 && p.Width <= 520) &&
		(p.Rounding == 0 || p.Rounding >= 4 && p.Rounding <= 28)
}

func (p OverlayAppearance) RendererSettings(launcherAccent string) string {
	accent := p.Accent
	if p.UseLauncherAccent && themeColor.MatchString(launcherAccent) {
		accent = launcherAccent
	}
	return fmt.Sprintf("%s,%s,%s,%s,%s,%d,%d,%d,%d,%d,%d",
		p.Position, p.Material, p.Surface, p.Text, accent, p.Opacity, p.Blur, p.Dim, p.Scale, p.Width, p.Rounding)
}

type Appearance struct {
	Overlay              OverlayAppearance `json:"overlay"`
	FontFile             string            `json:"fontFile"`
	BorderWidth          int               `json:"borderWidth"`
	BorderOpacity        int               `json:"borderOpacity"`
	Shadow               string            `json:"shadow"`
	ShadowStrength       int               `json:"shadowStrength"`
	Vignette             int               `json:"vignette"`
	Grain                int               `json:"grain"`
	GradientStyle        string            `json:"gradientStyle"`
	GradientShape        string            `json:"gradientShape"`
	GradientAngle        int               `json:"gradientAngle"`
	GradientOpacity      int               `json:"gradientOpacity"`
	GradientStart        string            `json:"gradientStart"`
	GradientMiddle       string            `json:"gradientMiddle"`
	GradientEnd          string            `json:"gradientEnd"`
	BarColor             string            `json:"barColor"`
	MutedColor           string            `json:"mutedColor"`
	NavColor             string            `json:"navColor"`
	NavText              string            `json:"navText"`
	NavActive            string            `json:"navActive"`
	NavOpacity           int               `json:"navOpacity"`
	NavBlur              int               `json:"navBlur"`
	NavHeight            int               `json:"navHeight"`
	NavTabWidth          int               `json:"navTabWidth"`
	NavTabHeight         int               `json:"navTabHeight"`
	NavTabRadius         int               `json:"navTabRadius"`
	NavTabGap            int               `json:"navTabGap"`
	NavTextSize          int               `json:"navTextSize"`
	ProfileColor         string            `json:"profileColor"`
	ProfileText          string            `json:"profileText"`
	ProfileControl       string            `json:"profileControl"`
	ProfileGradientOff   bool              `json:"profileGradientOff"`
	Theme                string            `json:"theme"`
	Font                 string            `json:"font"`
	Accent               string            `json:"accent"`
	Opacity              int               `json:"opacity"`
	Blur                 int               `json:"blur"`
	Radius               int               `json:"radius"`
	Dim                  int               `json:"dim"`
	Fit                  string            `json:"fit"`
	Gradient             bool              `json:"gradient"`
	Animate              bool              `json:"animate"`
	Background           string            `json:"background"`
	CanvasColor          string            `json:"canvasColor"`
	PanelColor           string            `json:"panelColor"`
	TextColor            string            `json:"textColor"`
	Position             string            `json:"position"`
	Compact              bool              `json:"compact"`
	Borders              bool              `json:"borders"`
	TextScale            int               `json:"textScale"`
	TextWeight           int               `json:"textWeight"`
	ButtonRadius         int               `json:"buttonRadius"`
	BackgroundBlur       int               `json:"backgroundBlur"`
	BackgroundEdgeBlur   int               `json:"videoEdgeBlur"` // Preserve settings from the video-only editor.
	BackgroundSaturation int               `json:"backgroundSaturation"`
	BackgroundZoom       int               `json:"backgroundZoom"`
	BackgroundBrightness int               `json:"backgroundBrightness"`
	BackgroundContrast   int               `json:"backgroundContrast"`
	BackgroundHue        int               `json:"backgroundHue"`
	BackgroundGrayscale  int               `json:"backgroundGrayscale"`
	BackgroundSepia      int               `json:"backgroundSepia"`
	BackgroundOpacity    int               `json:"backgroundOpacity"`
	GradientX            int               `json:"gradientX"`
	GradientY            int               `json:"gradientY"`
	GradientSize         int               `json:"gradientSize"`
	MotionSpeed          int               `json:"motionSpeed"`
	BackgroundEffect     string            `json:"backgroundEffect"`
	WaveColor            string            `json:"waveColor"`
	WaveStyle            string            `json:"waveStyle"`
	WaveIntensity        int               `json:"waveIntensity"`
	WaveAmplitude        int               `json:"waveAmplitude"`
	WaveTrail            int               `json:"waveTrail"`
	WaveOffset           int               `json:"waveOffset"`
	GlintsEnabled        bool              `json:"glintsEnabled"`
	GlintCount           int               `json:"glintCount"`
	GlintSize            int               `json:"glintSize"`
	GlintStrength        int               `json:"glintStrength"`
	GlintSpeed           int               `json:"glintSpeed"`
	GlintColor           string            `json:"glintColor"`
	ParticlesEnabled     bool              `json:"particlesEnabled"`
	ParticleCount        int               `json:"particleCount"`
	ParticleMinSize      int               `json:"particleMinSize"`
	ParticleMaxSize      int               `json:"particleMaxSize"`
	ParticleOpacity      int               `json:"particleOpacity"`
	ParticleFade         int               `json:"particleFade"`
	ParticleSpeedX       int               `json:"particleSpeedX"`
	ParticleSpeedY       int               `json:"particleSpeedY"`
	ParticleDepth        int               `json:"particleDepth"`
	ParticleColor        string            `json:"particleColor"`
}

func Default() Appearance {
	return Appearance{Overlay: DefaultOverlay(), Theme: "original", Font: "default", Accent: "#ededed", BorderWidth: 1, BorderOpacity: 8, Shadow: "off", ShadowStrength: 40, GradientStyle: "classic", GradientShape: "linear",
		GradientAngle: 145, GradientOpacity: 45, GradientStart: "#6142a5", GradientMiddle: "#3957a2", GradientEnd: "#151329",
		BarColor: "#101010", MutedColor: "#a0a0a0",
		NavOpacity: 100, NavHeight: 55, NavTabWidth: 130, NavTabHeight: 39, NavTabRadius: 11, NavTabGap: 52, NavTextSize: 15,
		Opacity: 100, Radius: 14, Dim: 45, Fit: "cover", Gradient: true, Animate: true, CanvasColor: "#090909", PanelColor: "#191919", TextColor: "#ededed", Position: "center",
		TextScale: 100, TextWeight: 600, ButtonRadius: 10, BackgroundSaturation: 100, BackgroundZoom: 100, BackgroundBrightness: 100, BackgroundContrast: 100, BackgroundOpacity: 100, GradientX: 12, GradientY: 48, GradientSize: 100, MotionSpeed: 50,
		BackgroundEffect: "gradient", WaveColor: "#ffffff", WaveStyle: "smooth", WaveIntensity: 100, WaveAmplitude: 100, WaveTrail: 50,
		GlintsEnabled: false, GlintCount: 5, GlintSize: 110, GlintStrength: 24, GlintSpeed: 22, GlintColor: "#a9c7ff",
		ParticlesEnabled: true, ParticleCount: 54, ParticleMinSize: 1, ParticleMaxSize: 4, ParticleOpacity: 36, ParticleFade: 100,
		ParticleSpeedX: -18, ParticleSpeedY: 0, ParticleDepth: 70, ParticleColor: "#e1ebff"}
}

func (p Appearance) Valid() bool {
	for _, color := range []string{p.NavColor, p.NavText, p.NavActive, p.ProfileColor, p.ProfileText, p.ProfileControl} {
		if color != "" && !themeColor.MatchString(color) {
			return false
		}
	}
	return p.Overlay.Valid() && (p.Theme == "original" || p.Theme == "slate" || p.Theme == "midnight" || p.Theme == "warm") &&
		(p.Font == "default" || p.Font == "segoe" || p.Font == "arial" || p.Font == "georgia" || p.Font == "mono" || p.Font == "verdana" || p.Font == "calibri" || (p.Font == "custom" && p.FontFile != "")) &&
		themeColor.MatchString(p.Accent) && themeColor.MatchString(p.CanvasColor) &&
		themeColor.MatchString(p.PanelColor) && themeColor.MatchString(p.TextColor) &&
		(p.Position == "center" || p.Position == "top" || p.Position == "bottom" || p.Position == "left" || p.Position == "right") &&
		p.Opacity >= 35 && p.Opacity <= 100 && p.Blur >= 0 && p.Blur <= 30 && p.Radius >= 0 && p.Radius <= 32 &&
		p.Dim >= 0 && p.Dim <= 90 && (p.Fit == "cover" || p.Fit == "contain" || p.Fit == "fill") &&
		(p.Background == "" || backgroundFile.MatchString(p.Background)) &&
		(p.FontFile == "" || appearanceFontFile.MatchString(p.FontFile)) &&
		themeColor.MatchString(p.BarColor) && themeColor.MatchString(p.MutedColor) &&
		themeColor.MatchString(p.GradientStart) && themeColor.MatchString(p.GradientMiddle) && themeColor.MatchString(p.GradientEnd) &&
		(p.GradientStyle == "classic" || p.GradientStyle == "aurora" || p.GradientStyle == "ember" || p.GradientStyle == "ocean" || p.GradientStyle == "custom") &&
		(p.GradientShape == "linear" || p.GradientShape == "radial" || p.GradientShape == "conic") &&
		p.GradientAngle >= 0 && p.GradientAngle <= 360 && p.GradientOpacity >= 0 && p.GradientOpacity <= 100 &&
		p.Vignette >= 0 && p.Vignette <= 85 && p.Grain >= 0 && p.Grain <= 8 &&
		p.BorderWidth >= 1 && p.BorderWidth <= 2 && p.BorderOpacity >= 0 && p.BorderOpacity <= 50 &&
		(p.Shadow == "off" || p.Shadow == "soft" || p.Shadow == "glow") && p.ShadowStrength >= 0 && p.ShadowStrength <= 100 &&
		p.TextScale >= 85 && p.TextScale <= 125 && p.TextWeight >= 400 && p.TextWeight <= 800 &&
		p.ButtonRadius >= 0 && p.ButtonRadius <= 24 && p.BackgroundBlur >= 0 && p.BackgroundBlur <= 24 &&
		p.BackgroundEdgeBlur >= 0 && p.BackgroundEdgeBlur <= 24 &&
		p.BackgroundSaturation >= 0 && p.BackgroundSaturation <= 250 && p.BackgroundZoom >= 100 && p.BackgroundZoom <= 150 &&
		p.BackgroundBrightness >= 30 && p.BackgroundBrightness <= 200 && p.BackgroundContrast >= 30 && p.BackgroundContrast <= 200 &&
		p.BackgroundHue >= 0 && p.BackgroundHue <= 360 && p.BackgroundGrayscale >= 0 && p.BackgroundGrayscale <= 100 &&
		p.BackgroundSepia >= 0 && p.BackgroundSepia <= 100 && p.BackgroundOpacity >= 0 && p.BackgroundOpacity <= 100 &&
		p.GradientX >= 0 && p.GradientX <= 100 && p.GradientY >= 0 && p.GradientY <= 100 &&
		p.GradientSize >= 50 && p.GradientSize <= 180 && p.MotionSpeed >= 10 && p.MotionSpeed <= 100 &&
		(p.BackgroundEffect == "wave" || p.BackgroundEffect == "gradient") && themeColor.MatchString(p.WaveColor) &&
		(p.WaveStyle == "smooth" || p.WaveStyle == "pixel") &&
		p.WaveIntensity >= 0 && p.WaveIntensity <= 130 && p.WaveAmplitude >= 0 && p.WaveAmplitude <= 160 && p.WaveTrail >= 0 && p.WaveTrail <= 100 &&
		p.WaveOffset >= 0 && p.WaveOffset <= 240 &&
		themeColor.MatchString(p.GlintColor) && p.GlintCount >= 0 && p.GlintCount <= 8 && p.GlintSize >= 20 && p.GlintSize <= 220 &&
		p.GlintStrength >= 0 && p.GlintStrength <= 100 && p.GlintSpeed >= 0 && p.GlintSpeed <= 100 &&
		themeColor.MatchString(p.ParticleColor) && p.ParticleCount >= 0 && p.ParticleCount <= 180 &&
		p.ParticleMinSize >= 1 && p.ParticleMinSize <= 16 && p.ParticleMaxSize >= p.ParticleMinSize && p.ParticleMaxSize <= 32 &&
		p.ParticleOpacity >= 0 && p.ParticleOpacity <= 100 && p.ParticleFade >= 0 && p.ParticleFade <= 100 &&
		p.ParticleSpeedX >= -100 && p.ParticleSpeedX <= 100 && p.ParticleSpeedY >= -100 && p.ParticleSpeedY <= 100 &&
		p.ParticleDepth >= 0 && p.ParticleDepth <= 100 &&
		p.NavOpacity >= 35 && p.NavOpacity <= 100 && p.NavBlur >= 0 && p.NavBlur <= 30 &&
		p.NavHeight >= 52 && p.NavHeight <= 76 && p.NavTabWidth >= 105 && p.NavTabWidth <= 175 &&
		p.NavTabHeight >= 28 && p.NavTabHeight <= 46 && p.NavTabRadius >= 0 && p.NavTabRadius <= 24 &&
		p.NavTabGap >= 8 && p.NavTabGap <= 86 && p.NavTextSize >= 12 && p.NavTextSize <= 20
}
