package api

// fenToYuan converts integer fen (分) to yuan string.
func fenToYuan(fen int) string {
	neg := fen < 0
	if neg {
		fen = -fen
	}
	if fen == 0 {
		return "0.00"
	}
	yuan := fen / 100
	cent := fen % 100
	s := ""
	if neg {
		s = "-"
	}
	return s + itoa(yuan) + "." + twoDigits(cent)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + string(byte('0'+n))
	}
	return itoa(n)
}
