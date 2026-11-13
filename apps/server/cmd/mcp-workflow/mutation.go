package main

import "strconv"

func validDecimalRevision(value string) bool {
	if value == "" || len(value) > 20 || (len(value) > 1 && value[0] == '0') {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 64)
	return err == nil
}

func nextDecimalRevision(previous, next string) bool {
	if !validDecimalRevision(previous) || !validDecimalRevision(next) {
		return false
	}
	before, err := strconv.ParseUint(previous, 10, 64)
	if err != nil {
		return false
	}
	after, err := strconv.ParseUint(next, 10, 64)
	return err == nil && before != ^uint64(0) && after == before+1
}
