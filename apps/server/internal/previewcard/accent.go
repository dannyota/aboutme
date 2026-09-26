package previewcard

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

// accentContrast is the contrast the card accent keeps against the white
// card, the level solid-color rule of the resume renderer.
const accentContrast = 3.0

var errColor = errors.New("previewcard: invalid color")

// Accent is the template accent (colors.accent, else colors.primary) as a
// lowercase hex color, clamped to 3:1 against white the way the resume
// renderer clamps its solid accent: darken in OKLCH lightness steps of 0.005
// until the ratio holds, else black.
func Accent(colors schema.Colors) (string, error) {
	source := colors.Primary
	if colors.Accent != nil {
		source = *colors.Accent
	}
	source = strings.ToLower(source)
	if _, err := parseHex(source); err != nil {
		return "", err
	}
	if contrastWithWhite(source) >= accentContrast {
		return source, nil
	}
	start := toOKLCH(source)
	for lightness := start.l - 0.005; lightness > 0; lightness -= 0.005 {
		candidate := fromOKLCH(oklch{l: lightness, c: start.c, h: start.h})
		if contrastWithWhite(candidate) >= accentContrast {
			return candidate, nil
		}
	}
	return "#000000", nil
}

type rgb struct{ r, g, b float64 }

type oklch struct{ l, c, h float64 }

func parseHex(color string) (rgb, error) {
	if len(color) != 7 || color[0] != '#' {
		return rgb{}, errColor
	}
	var channels [3]float64
	for index := range channels {
		value, err := strconv.ParseUint(color[1+2*index:3+2*index], 16, 8)
		if err != nil {
			return rgb{}, errColor
		}
		channels[index] = float64(value) / 255
	}
	return rgb{r: channels[0], g: channels[1], b: channels[2]}, nil
}

// mustParseHex parses a color Accent already validated or built itself.
func mustParseHex(color string) rgb {
	value, err := parseHex(color)
	if err != nil {
		panic("previewcard: unvalidated color")
	}
	return value
}

func toLinear(value float64) float64 {
	if value <= 0.04045 {
		return value / 12.92
	}
	return math.Pow((value+0.055)/1.055, 2.4)
}

func fromLinear(value float64) float64 {
	if value <= 0.0031308 {
		return 12.92 * value
	}
	return 1.055*math.Pow(value, 1/2.4) - 0.055
}

func toHex(color rgb) string {
	channel := func(value float64) int {
		return int(math.Round(math.Min(1, math.Max(0, value)) * 255))
	}
	return fmt.Sprintf("#%02x%02x%02x", channel(color.r), channel(color.g), channel(color.b))
}

func contrastWithWhite(color string) float64 {
	value := mustParseHex(color)
	luminance := 0.2126*toLinear(value.r) + 0.7152*toLinear(value.g) + 0.0722*toLinear(value.b)
	return 1.05 / (luminance + 0.05)
}

func toOKLCH(color string) oklch {
	value := mustParseHex(color)
	r, g, b := toLinear(value.r), toLinear(value.g), toLinear(value.b)
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	labA := 1.9779984951*l - 2.428592205*m + 0.4505937099*s
	labB := 0.0259040371*l + 0.7827717662*m - 0.808675766*s
	return oklch{
		l: 0.2104542553*l + 0.793617785*m - 0.0040720468*s,
		c: math.Hypot(labA, labB),
		h: math.Atan2(labB, labA),
	}
}

func cube(value float64) float64 { return value * value * value }

func fromOKLCH(color oklch) string {
	labA, labB := color.c*math.Cos(color.h), color.c*math.Sin(color.h)
	l := cube(color.l + 0.3963377774*labA + 0.2158037573*labB)
	m := cube(color.l - 0.1055613458*labA - 0.0638541728*labB)
	s := cube(color.l - 0.0894841775*labA - 1.291485548*labB)
	return toHex(rgb{
		r: fromLinear(4.0767416621*l - 3.3077115913*m + 0.2309699292*s),
		g: fromLinear(-1.2684380046*l + 2.6097574011*m - 0.3413193965*s),
		b: fromLinear(-0.0041960863*l - 0.7034186147*m + 1.707614701*s),
	})
}
