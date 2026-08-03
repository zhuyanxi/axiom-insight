package generator

import "strconv"

func itoa(value int) string {
	return strconv.Itoa(value)
}

func strconvQuote(value string) string {
	return strconv.Quote(value)
}
