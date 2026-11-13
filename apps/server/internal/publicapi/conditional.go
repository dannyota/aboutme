package publicapi

import "net/http"

func parseIfNoneMatch(header http.Header) (string, bool, bool) {
	values, ok := header["If-None-Match"]
	if !ok {
		return "", false, true
	}
	if len(values) != 1 {
		return "", true, false
	}
	value := values[0]
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", true, false
	}
	for i := 1; i < len(value)-1; i++ {
		if value[i] < 0x21 || value[i] == 0x7f || value[i] == '"' {
			return "", true, false
		}
	}
	return value, true, true
}
