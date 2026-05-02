package tui

import tea "github.com/charmbracelet/bubbletea"

// layoutMap translates a Cyrillic or Greek rune produced by a non-Latin
// keyboard layout into the Latin character that occupies the same
// physical key. Used by translateLayout to make global hotkeys
// (`[`, `]`, `q`, `n`, …) work regardless of which layout the user has
// active. Translation is suppressed when a text input is focused, so
// users can still type Russian/Greek into comments, search, and
// filter inputs.
var layoutMap = map[rune]rune{
	// --- Russian QWERTY (JCUKEN) ---
	'й': 'q', 'ц': 'w', 'у': 'e', 'к': 'r', 'е': 't',
	'н': 'y', 'г': 'u', 'ш': 'i', 'щ': 'o', 'з': 'p',
	'х': '[', 'ъ': ']',
	'ф': 'a', 'ы': 's', 'в': 'd', 'а': 'f', 'п': 'g',
	'р': 'h', 'о': 'j', 'л': 'k', 'д': 'l', 'ж': ';', 'э': '\'',
	'я': 'z', 'ч': 'x', 'с': 'c', 'м': 'v', 'и': 'b',
	'т': 'n', 'ь': 'm', 'б': ',', 'ю': '.',
	'ё': '`',
	// Russian uppercase (Shift)
	'Й': 'Q', 'Ц': 'W', 'У': 'E', 'К': 'R', 'Е': 'T',
	'Н': 'Y', 'Г': 'U', 'Ш': 'I', 'Щ': 'O', 'З': 'P',
	'Х': '{', 'Ъ': '}',
	'Ф': 'A', 'Ы': 'S', 'В': 'D', 'А': 'F', 'П': 'G',
	'Р': 'H', 'О': 'J', 'Л': 'K', 'Д': 'L', 'Ж': ':', 'Э': '"',
	'Я': 'Z', 'Ч': 'X', 'С': 'C', 'М': 'V', 'И': 'B',
	'Т': 'N', 'Ь': 'M', 'Б': '<', 'Ю': '>',
	'Ё': '~',

	// --- Greek QWERTY (modern Greek layout) ---
	'ς': 'w', 'ε': 'e', 'ρ': 'r', 'τ': 't',
	'υ': 'y', 'θ': 'u', 'ι': 'i', 'ο': 'o', 'π': 'p',
	'α': 'a', 'σ': 's', 'δ': 'd', 'φ': 'f', 'γ': 'g',
	'η': 'h', 'ξ': 'j', 'κ': 'k', 'λ': 'l',
	'ζ': 'z', 'χ': 'x', 'ψ': 'c', 'ω': 'v', 'β': 'b',
	'ν': 'n', 'μ': 'm',
	// Greek uppercase
	'Σ': 'S', 'Ε': 'E', 'Ρ': 'R', 'Τ': 'T',
	'Υ': 'Y', 'Θ': 'U', 'Ι': 'I', 'Ο': 'O', 'Π': 'P',
	'Α': 'A', 'Δ': 'D', 'Φ': 'F', 'Γ': 'G',
	'Η': 'H', 'Ξ': 'J', 'Κ': 'K', 'Λ': 'L',
	'Ζ': 'Z', 'Χ': 'X', 'Ψ': 'C', 'Ω': 'V', 'Β': 'B',
	'Ν': 'N', 'Μ': 'M',
}

// translateLayout returns msg with Cyrillic/Greek runes mapped to their
// Latin physical-position equivalents. Returns msg unchanged when no
// rune in the message is in the layoutMap, when Type != KeyRunes, or
// when Runes is empty. The original msg is never mutated.
func translateLayout(msg tea.KeyMsg) tea.KeyMsg {
	if msg.Type != tea.KeyRunes || len(msg.Runes) == 0 {
		return msg
	}
	translated := make([]rune, len(msg.Runes))
	changed := false
	for i, r := range msg.Runes {
		if latin, ok := layoutMap[r]; ok {
			translated[i] = latin
			changed = true
		} else {
			translated[i] = r
		}
	}
	if !changed {
		return msg
	}
	out := msg
	out.Runes = translated
	return out
}
