package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"strconv"
	"strings"
)

var invalid = errors.New("invalid_allowance")

func strictJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var read func() (any, error)
	read = func() (any, error) {
		token, err := decoder.Token()
		if err != nil {
			return nil, invalid
		}
		switch token {
		case json.Delim('{'):
			object := make(map[string]any)
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return nil, invalid
				}
				name, ok := key.(string)
				if !ok {
					return nil, invalid
				}
				if _, exists := object[name]; exists {
					return nil, invalid
				}
				value, err := read()
				if err != nil {
					return nil, err
				}
				object[name] = value
			}
			if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
				return nil, invalid
			}
			return object, nil
		case json.Delim('['):
			array := make([]any, 0)
			for decoder.More() {
				value, err := read()
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			if closing, err := decoder.Token(); err != nil || closing != json.Delim(']') {
				return nil, invalid
			}
			return array, nil
		default:
			return token, nil
		}
	}
	value, err := read()
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, invalid
	}
	return value, nil
}

func rows(raw []byte) ([]any, error) {
	value, err := strictJSON(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok || object["success"] != true {
		return nil, invalid
	}
	if next, exists := object["isNext"]; exists && next != false {
		return nil, invalid
	}
	var result []any
	found := false
	for _, key := range []string{"data", "datas"} {
		if object[key] == nil {
			continue
		}
		array, ok := object[key].([]any)
		if !ok || found {
			return nil, invalid
		}
		result, found = array, true
	}
	if !found {
		return nil, invalid
	}
	return result, nil
}

func regionIdentity(raw []byte) (string, error) {
	array, err := rows(raw)
	if err != nil {
		return "", err
	}
	seen := make(map[string]bool)
	selected := ""
	for _, item := range array {
		row, ok := item.(map[string]any)
		if !ok {
			return "", invalid
		}
		id, idOK := row["regionId"].(string)
		name, nameOK := row["regionName"].(string)
		if !idOK || !nameOK || id == "" || name == "" || seen[id] {
			return "", invalid
		}
		seen[id] = true
		if strings.EqualFold(name, "HCM04") {
			if selected != "" {
				return "", invalid
			}
			selected = id
		}
	}
	if selected == "" {
		return "", invalid
	}
	return selected, nil
}

func projectNumbers(raw []byte, project, region string) (string, string, error) {
	array, err := rows(raw)
	if err != nil {
		return "", "", err
	}
	seen := make(map[string]bool)
	quota, usage := "", ""
	for _, item := range array {
		row, ok := item.(map[string]any)
		if !ok {
			return "", "", invalid
		}
		id, ok := row["projectId"].(string)
		if !ok || id == "" || seen[id] {
			return "", "", invalid
		}
		seen[id] = true
		if id != project {
			continue
		}
		if row["regionId"] != region || row["regionName"] != "HCM04" {
			return "", "", invalid
		}
		q, quotaOK := row["totalQuota"].(json.Number)
		u, usageOK := row["usage"].(json.Number)
		if !quotaOK || !usageOK {
			return "", "", invalid
		}
		quota, usage = q.String(), u.String()
		if _, err := gigabytes(quota, true); err != nil {
			return "", "", err
		}
		if _, err := gigabytes(usage, false); err != nil {
			return "", "", err
		}
	}
	if quota == "" || usage == "" {
		return "", "", invalid
	}
	return quota, usage, nil
}

func gigabytes(raw string, lower bool) (int64, error) {
	if len(raw) > 100 || strings.HasPrefix(raw, "-") {
		return 0, invalid
	}
	value, ok := new(big.Rat).SetString(raw)
	if !ok || value.Sign() < 0 {
		return 0, invalid
	}
	factor := int64(1073741824)
	if lower {
		factor = 1000000000
	}
	value.Mul(value, new(big.Rat).SetInt64(factor))
	integer, remainder := new(big.Int), new(big.Int)
	integer.QuoRem(value.Num(), value.Denom(), remainder)
	if !lower && remainder.Sign() != 0 {
		integer.Add(integer, big.NewInt(1))
	}
	if !integer.IsInt64() || integer.Sign() < 0 {
		return 0, invalid
	}
	return integer.Int64(), nil
}

func displayed(raw string) (map[string]any, error) {
	parts := strings.Split(strings.ToLower(raw), "e")
	precision := 0
	if point := strings.IndexByte(parts[0], '.'); point >= 0 {
		precision = len(parts[0]) - point - 1
	}
	if len(parts) == 2 {
		exponent, err := strconv.Atoi(parts[1])
		if err != nil {
			return nil, invalid
		}
		precision -= exponent
	}
	if precision < 0 {
		precision = 0
	}
	if precision > 32 {
		return nil, invalid
	}
	value, ok := new(big.Rat).SetString(raw)
	if !ok {
		return nil, invalid
	}
	return map[string]any{"display": value.FloatString(precision), "unit": "GB", "precision": precision, "rounded": false}, nil
}
