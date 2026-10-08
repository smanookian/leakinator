package secretengine

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

// MinSecretLength is the shortest .env value that counts as a secret.
// Shorter values ("3000", "dev") would cause false alarms.
const MinSecretLength = 8

// LoadEnvFile reads secrets from a .env file. See ParseEnv.
func LoadEnvFile(path string) ([]Secret, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseEnv(f, path)
}

// ParseEnv reads KEY=VALUE lines. It understands comments, "export ",
// and single, double and multi-line quoted values. Values that can't be
// secrets are skipped: shorter than MinSecretLength, plain numbers,
// and true/false. Errors never contain values.
func ParseEnv(r io.Reader, source string) ([]Secret, error) {
	var out []Secret
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if value != "" && (value[0] == '"' || value[0] == '\'') {
			q := value[0]
			value = value[1:]
			// Multi-line value: read until the closing quote.
			for !closes(value, q) {
				if !sc.Scan() {
					return nil, fmt.Errorf("%s:%d: %s has no closing quote", source, lineNo, name)
				}
				lineNo++
				value += "\n" + sc.Text()
			}
			value = value[:closeIndex(value, q)]
			if q == '"' {
				value = strings.NewReplacer(`\n`, "\n", `\"`, `"`, `\\`, `\`).Replace(value)
			}
		} else if i := strings.Index(value, " #"); i >= 0 {
			value = strings.TrimSpace(value[:i])
		}
		if isSecretValue(value) {
			out = append(out, Secret{Name: name, Value: value, Source: source})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return out, nil
}

func closeIndex(s string, q byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && q == '"' {
			i++
			continue
		}
		if s[i] == q {
			return i
		}
	}
	return -1
}

func closes(s string, q byte) bool { return closeIndex(s, q) >= 0 }

func isSecretValue(v string) bool {
	if utf8.RuneCountInString(v) < MinSecretLength {
		return false
	}
	switch strings.ToLower(v) {
	case "true", "false":
		return false
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return false
	}
	return true
}
