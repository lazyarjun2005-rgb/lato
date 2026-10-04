// Package theme contains Lato's named semantic color palettes.
package theme

import "strings"

const DefaultName = "electric-blue"

// Palette describes the semantic colors used by the TUI. Empty colors are
// intentional: lipgloss leaves the terminal's normal foreground/background
// untouched, which is how the system theme preserves terminal defaults.
type Palette struct {
	Primary    string
	Assistant  string
	User       string
	Border     string
	SelectedFG string
	SelectedBG string
	Prompt     string
	Muted      string
	Text       string
	Success    string
	Warning    string
	Error      string
	Info       string
}

var palettes = map[string]Palette{
	"electric-blue":        {"#0000FF", "#0000FF", "#0000FF", "#0000FF", "#EAEAEA", "#000033", "#0000FF", "#7A7A7A", "#EAEAEA", "#65D1FF", "#FFD166", "#FF6B6B", "#8AB4F8"},
	"aura":                 {"#A277FF", "#A277FF", "#61FFCA", "#A277FF", "#F8F8F2", "#302A4C", "#A277FF", "#8B82A8", "#EDE7F6", "#61FFCA", "#FFCA85", "#FF6767", "#82E2FF"},
	"ayu":                  {"#E6B450", "#95E6CB", "#F07178", "#E6B450", "#E6E1CF", "#3D412F", "#E6B450", "#626A73", "#E6E1CF", "#AAD94C", "#FFB454", "#F07178", "#59C2FF"},
	"ayu-light":            {"#F8F9FA", "#5C6773", "#F07171", "#E6B450", "#5C6773", "#E7E8E9", "#E6B450", "#86919C", "#5C6773", "#86B300", "#F2AE49", "#F07171", "#55B4D4"},
	"ayu-mirage":           {"#FFCC66", "#CBCCC6", "#F28779", "#FFCC66", "#CBCCC6", "#242936", "#FFCC66", "#707A8C", "#CBCCC6", "#D5FF80", "#FFCC66", "#F28779", "#80D4FF"},
	"carbonfox":            {"#78A9FF", "#BE95FF", "#33B1FF", "#78A9FF", "#F2F4F8", "#2A2A3A", "#78A9FF", "#7D8590", "#F2F4F8", "#42BE65", "#F1C21B", "#FA4D56", "#82CFFF"},
	"catppuccin":           {"#CBA6F7", "#CBA6F7", "#89B4FA", "#CBA6F7", "#CDD6F4", "#45475A", "#CBA6F7", "#7F849C", "#CDD6F4", "#A6E3A1", "#F9E2AF", "#F38BA8", "#89DCEB"},
	"catppuccin-frappe":    {"#CA9EE6", "#CA9EE6", "#8CAAEE", "#CA9EE6", "#C6D0F5", "#51576D", "#CA9EE6", "#838BA7", "#C6D0F5", "#A6D189", "#E5C890", "#E78284", "#81C8BE"},
	"catppuccin-macchiato": {"#C6A0F6", "#C6A0F6", "#8AADF4", "#C6A0F6", "#CAD3F5", "#494D64", "#C6A0F6", "#8087A2", "#CAD3F5", "#A6DA95", "#EED49F", "#ED8796", "#8BD5CA"},
	"catppuccin-latte":     {"#8839EF", "#8839EF", "#1E66F5", "#8839EF", "#4C4F69", "#DCE0E8", "#8839EF", "#8C8FA1", "#4C4F69", "#40A02B", "#DF8E1D", "#D20F39", "#179299"},
	"catppuccin-mocha":     {"#CBA6F7", "#CBA6F7", "#89B4FA", "#CBA6F7", "#CDD6F4", "#313244", "#CBA6F7", "#7F849C", "#CDD6F4", "#A6E3A1", "#F9E2AF", "#F38BA8", "#94E2D5"},
	"cobalt2":              {"#0088FF", "#FF9D00", "#3AD900", "#0088FF", "#FFFFFF", "#193549", "#0088FF", "#7F9BA6", "#FFFFFF", "#3AD900", "#FFDD00", "#FF628C", "#80CBC4"},
	"cursor":               {"#7C3AED", "#A78BFA", "#22D3EE", "#7C3AED", "#FFFFFF", "#312E81", "#7C3AED", "#94A3B8", "#F8FAFC", "#34D399", "#FBBF24", "#FB7185", "#38BDF8"},
	"dracula":              {"#BD93F9", "#FF79C6", "#8BE9FD", "#BD93F9", "#F8F8F2", "#44475A", "#BD93F9", "#6272A4", "#F8F8F2", "#50FA7B", "#F1FA8C", "#FF5555", "#8BE9FD"},
	"everforest":           {"#A7C080", "#7FBBB3", "#DBBC7F", "#A7C080", "#D3C6AA", "#4B5440", "#A7C080", "#859289", "#D3C6AA", "#A7C080", "#DBBC7F", "#E67E80", "#83C092"},
	"flexoki":              {"#205EA6", "#AF3029", "#66800B", "#205EA6", "#FFF8E8", "#DAD8CE", "#205EA6", "#878580", "#100F0F", "#66800B", "#AD8301", "#AF3029", "#24837B"},
	"github":               {"#0969DA", "#8250DF", "#1A7F37", "#D0D7DE", "#24292F", "#D8DEE4", "#0969DA", "#656D76", "#24292F", "#1A7F37", "#9A6700", "#CF222E", "#0969DA"},
	"github-light":         {"#0969DA", "#8250DF", "#1A7F37", "#D0D7DE", "#24292F", "#F6F8FA", "#0969DA", "#656D76", "#24292F", "#1A7F37", "#9A6700", "#CF222E", "#0969DA"},
	"gruvbox":              {"#D79921", "#83A598", "#B8BB26", "#D79921", "#EBDBB2", "#504945", "#D79921", "#928374", "#EBDBB2", "#B8BB26", "#FABD2F", "#FB4934", "#83A598"},
	"gruvbox-light":        {"#B57614", "#076678", "#79740E", "#B57614", "#3C3836", "#EBDBB2", "#B57614", "#928374", "#3C3836", "#79740E", "#B57614", "#9D0006", "#427B58"},
	"kanagawa":             {"#7E9CD8", "#957FB8", "#7AA89F", "#7E9CD8", "#DCD7BA", "#363646", "#7E9CD8", "#727169", "#DCD7BA", "#98BB6C", "#E6C384", "#E82424", "#7FB4CA"},
	"lucent-orng":          {"#FF8700", "#FF9E64", "#FFD580", "#FF8700", "#FFF4E6", "#4A2A16", "#FF8700", "#A88B78", "#FFF4E6", "#A8CC8C", "#FFD580", "#FF6B6B", "#80C8FF"},
	"material":             {"#82AAFF", "#C792EA", "#89DDFF", "#82AAFF", "#EEFFFF", "#263238", "#82AAFF", "#546E7A", "#EEFFFF", "#C3E88D", "#FFCB6B", "#F07178", "#89DDFF"},
	"matrix":               {"#00FF41", "#00FF41", "#00CC33", "#00FF41", "#00FF41", "#003B00", "#00FF41", "#008F11", "#00FF41", "#00FF41", "#B5FF00", "#FF3030", "#00CCFF"},
	"mercury":              {"#4F46E5", "#7C3AED", "#0891B2", "#CBD5E1", "#1E293B", "#E2E8F0", "#4F46E5", "#64748B", "#1E293B", "#059669", "#D97706", "#DC2626", "#0284C7"},
	"monokai":              {"#F92672", "#F92672", "#A6E22E", "#F92672", "#F8F8F2", "#49483E", "#F92672", "#75715E", "#F8F8F2", "#A6E22E", "#E6DB74", "#F92672", "#66D9EF"},
	"nightowl":             {"#82AAFF", "#C792EA", "#7FDBCA", "#82AAFF", "#D6DEEB", "#1B2B44", "#82AAFF", "#5F7E97", "#D6DEEB", "#ADDB67", "#ECC48D", "#EF5350", "#80CBC4"},
	"nord":                 {"#88C0D0", "#B48EAD", "#A3BE8C", "#88C0D0", "#ECEFF4", "#434C5E", "#88C0D0", "#81A1C1", "#D8DEE9", "#A3BE8C", "#EBCB8B", "#BF616A", "#5E81AC"},
	"nord-light":           {"#5E81AC", "#B48EAD", "#A3BE8C", "#5E81AC", "#2E3440", "#E5E9F0", "#5E81AC", "#616E88", "#2E3440", "#A3BE8C", "#D08770", "#BF616A", "#81A1C1"},
	"one-dark":             {"#61AFEF", "#C678DD", "#98C379", "#61AFEF", "#ABB2BF", "#3E4451", "#61AFEF", "#5C6370", "#ABB2BF", "#98C379", "#E5C07B", "#E06C75", "#56B6C2"},
	"opencode":             {"#FF7A00", "#FF7A00", "#7DD3FC", "#FF7A00", "#F8FAFC", "#3B2415", "#FF7A00", "#94A3B8", "#E2E8F0", "#4ADE80", "#FACC15", "#FB7185", "#38BDF8"},
	"orng":                 {"#FF8800", "#FFAA33", "#FFD166", "#FF8800", "#FFF7ED", "#572D12", "#FF8800", "#A8A29E", "#292524", "#84CC16", "#FACC15", "#EF4444", "#38BDF8"},
	"osaka-jade":           {"#68D391", "#9F7AEA", "#81E6D9", "#68D391", "#E6FFFA", "#234E52", "#68D391", "#68A0A0", "#E6FFFA", "#9AE6B4", "#F6E05E", "#FC8181", "#63B3ED"},
	"palenight":            {"#82AAFF", "#C792EA", "#89DDFF", "#82AAFF", "#A6ACCD", "#444267", "#82AAFF", "#676E95", "#A6ACCD", "#C3E88D", "#FFCB6B", "#F07178", "#89DDFF"},
	"rosepine":             {"#C4A7E7", "#EBBCBA", "#9CCFD8", "#C4A7E7", "#E0DEF4", "#403D52", "#C4A7E7", "#6E6A86", "#E0DEF4", "#9CCFD8", "#F6C177", "#EB6F92", "#31748F"},
	"rosepine-dawn":        {"#907AA9", "#D7827E", "#56949F", "#907AA9", "#575279", "#F2E9DE", "#907AA9", "#9893A5", "#575279", "#56949F", "#EA9D34", "#B4637A", "#286983"},
	"rosepine-moon":        {"#C4A7E7", "#EABBB9", "#9CCFD8", "#C4A7E7", "#E0DEF4", "#393552", "#C4A7E7", "#817C9C", "#E0DEF4", "#9CCFD8", "#F6C177", "#EB6F92", "#3E8FB0"},
	"solarized":            {"#268BD2", "#6C71C4", "#859900", "#268BD2", "#839496", "#073642", "#268BD2", "#586E75", "#839496", "#859900", "#B58900", "#DC322F", "#2AA198"},
	"solarized-light":      {"#268BD2", "#6C71C4", "#859900", "#268BD2", "#073642", "#EEE8D5", "#268BD2", "#93A1A1", "#073642", "#859900", "#B58900", "#DC322F", "#2AA198"},
	"synthwave84":          {"#FF7EDB", "#FF7EDB", "#72F1B8", "#FF7EDB", "#FFFFFF", "#34294F", "#FF7EDB", "#848BBD", "#FFFFFF", "#72F1B8", "#FEDE5D", "#FE4450", "#36F9F6"},
	"system":               {"", "", "", "", "", "", "", "", "", "#00A000", "#A06000", "#C00000", "#0080A0"},
	"tokyonight":           {"#7AA2F7", "#BB9AF7", "#9ECE6A", "#7AA2F7", "#C0CAF5", "#3B4261", "#7AA2F7", "#565F89", "#C0CAF5", "#9ECE6A", "#E0AF68", "#F7768E", "#7DCFFF"},
	"tokyonight-light":     {"#2E7DE9", "#9854F1", "#587539", "#2E7DE9", "#3760BF", "#D5D6DB", "#2E7DE9", "#8990B3", "#3760BF", "#587539", "#8C6C3E", "#F52A65", "#007197"},
	"tokyonight-storm":     {"#7AA2F7", "#BB9AF7", "#9ECE6A", "#7AA2F7", "#C0CAF5", "#414868", "#7AA2F7", "#565F89", "#C0CAF5", "#9ECE6A", "#E0AF68", "#F7768E", "#7DCFFF"},
	"vercel":               {"#0070F3", "#7928CA", "#0070F3", "#EAEAEA", "#FFFFFF", "#EAEAEA", "#0070F3", "#666666", "#111111", "#0070F3", "#F5A623", "#E00", "#0070F3"},
	"vesper":               {"#FFC799", "#A0A0A0", "#99FFE4", "#FFC799", "#FFF3E4", "#3B3530", "#FFC799", "#8C8279", "#FFF3E4", "#99FFE4", "#FFC799", "#FF8080", "#80C8FF"},
	"zenburn":              {"#DFAF8F", "#DFAF8F", "#7F9F7F", "#DFAF8F", "#DCDCCC", "#4F4F4F", "#DFAF8F", "#7F807F", "#DCDCCC", "#7F9F7F", "#F0DFAF", "#CC9393", "#8CD0D3"},
}

// Names returns all registered names in stable lexical order.
func Names() []string {
	names := make([]string, 0, len(palettes))
	for name := range palettes {
		names = append(names, name)
	}
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	return names
}

// Lookup returns a copy of a named palette using case-insensitive matching.
func Lookup(name string) (Palette, bool) {
	p, ok := palettes[strings.ToLower(strings.TrimSpace(name))]
	return p, ok
}

// Resolve returns the requested palette, falling back safely to the default.
func Resolve(name string) (string, Palette) {
	key := strings.ToLower(strings.TrimSpace(name))
	if p, ok := palettes[key]; ok {
		return key, p
	}
	return DefaultName, palettes[DefaultName]
}
