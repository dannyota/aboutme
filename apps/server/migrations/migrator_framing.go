package migrations

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"
)

const firstProtectedMigrationVersion int64 = 14

var migrationSourceName = regexp.MustCompile(`^([0-9]+)_(.+)\.(sql|go)$`)

func validateProtectedMigrationSources(fsys fs.FS) error {
	if fsys == nil {
		return errors.New("migrations: nil migration filesystem")
	}
	versions := make(map[int64]string)
	for _, pattern := range []string{"*.sql", "*.go"} {
		names, err := fs.Glob(fsys, pattern)
		if err != nil {
			return fmt.Errorf("migrations: list %s sources: %w", pattern, err)
		}
		for _, name := range names {
			base := path.Base(name)
			match := migrationSourceName.FindStringSubmatch(base)
			if match == nil {
				if len(base) > 0 && base[0] >= '0' && base[0] <= '9' {
					return fmt.Errorf("migrations: malformed migration source %q", base)
				}
				continue
			}
			version, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil || version <= 0 {
				return fmt.Errorf("migrations: invalid migration version in %q", base)
			}
			if prior, exists := versions[version]; exists {
				return fmt.Errorf("migrations: duplicate migration version %d in %q and %q", version, prior, base)
			}
			versions[version] = base
			if version < firstProtectedMigrationVersion {
				continue
			}
			if match[3] != "sql" {
				return fmt.Errorf("migrations: protected Go migration %q is forbidden", base)
			}
			contents, err := fs.ReadFile(fsys, name)
			if err != nil {
				return fmt.Errorf("migrations: read %q: %w", base, err)
			}
			if err := validateProtectedSQLMigration(base, version, string(contents)); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateProtectedSQLMigration(name string, version int64, source string) error {
	up, transactional, err := protectedUpSource(source)
	if err != nil {
		return fmt.Errorf("migrations: protected source %q: %w", name, err)
	}
	if !transactional {
		return fmt.Errorf("migrations: protected source %q uses NO TRANSACTION", name)
	}
	statements, err := splitOuterSQLStatements(up)
	if err != nil {
		return fmt.Errorf("migrations: protected source %q: %w", name, err)
	}
	if len(statements) < 2 {
		return fmt.Errorf("migrations: protected source %q lacks framing statements", name)
	}
	transactionControl := regexp.MustCompile(`(?is)^(commit|end|rollback|abort|prepare\s+transaction|start\s+transaction|begin)(?:\s|$)`)
	for _, statement := range statements {
		if transactionControl.MatchString(strings.TrimSpace(statement)) {
			return fmt.Errorf("migrations: protected source %q contains transaction control", name)
		}
	}
	wantBegin := regexp.MustCompile(`(?is)^select\s+public\.runtime_begin_migration_write\s*\(\s*'` + regexp.QuoteMeta(fmt.Sprintf("migration-%05d", version)) + `'\s*\)\s*$`)
	if !wantBegin.MatchString(strings.TrimSpace(statements[0])) {
		return fmt.Errorf("migrations: protected source %q has invalid first Up statement", name)
	}
	wantFinish := regexp.MustCompile(`(?is)^select\s+public\.runtime_finish_write\s*\(\s*\)\s*$`)
	if !wantFinish.MatchString(strings.TrimSpace(statements[len(statements)-1])) {
		return fmt.Errorf("migrations: protected source %q has invalid last Up statement", name)
	}
	return nil
}

func protectedUpSource(source string) (string, bool, error) {
	scanner := bufio.NewScanner(strings.NewReader(source))
	var up strings.Builder
	inUp := false
	foundUp := false
	transactional := true
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") && strings.Contains(line, "+goose") {
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				return "", false, fmt.Errorf("goose annotation has leading whitespace: %q", line)
			}
			command := strings.ReplaceAll(line, "--", "")
			command = strings.Replace(command, "+goose", "", 1)
			if strings.Contains(command, "+goose") {
				return "", false, fmt.Errorf("multiple Goose annotations: %q", line)
			}
			switch {
			case strings.EqualFold(strings.TrimSpace(command), "NO TRANSACTION"):
				transactional = false
				continue
			case strings.EqualFold(strings.TrimSpace(command), "ENVSUB ON"):
				return "", false, errors.New("ENVSUB ON is forbidden")
			case strings.EqualFold(strings.TrimSpace(command), "ENVSUB OFF"):
				continue
			case strings.EqualFold(strings.TrimSpace(command), "Up"):
				if foundUp {
					return "", false, errors.New("duplicate Up annotation")
				}
				foundUp, inUp = true, true
				continue
			case strings.EqualFold(strings.TrimSpace(command), "Down"):
				inUp = false
				continue
			case strings.EqualFold(strings.TrimSpace(command), "StatementBegin"), strings.EqualFold(strings.TrimSpace(command), "StatementEnd"):
				continue
			default:
				return "", false, fmt.Errorf("unsupported Goose annotation: %q", line)
			}
		}
		if inUp {
			up.WriteString(line)
			up.WriteByte('\n')
		}
	}
	if err := scanner.Err(); err != nil {
		return "", false, err
	}
	if !foundUp {
		return "", false, errors.New("missing Up annotation")
	}
	return up.String(), transactional, nil
}

func splitOuterSQLStatements(source string) ([]string, error) {
	var statements []string
	var statement strings.Builder
	for i := 0; i < len(source); {
		switch {
		case strings.HasPrefix(source[i:], "--"):
			end := strings.IndexByte(source[i:], '\n')
			if end < 0 {
				i = len(source)
			} else {
				i += end + 1
				statement.WriteByte(' ')
			}
		case strings.HasPrefix(source[i:], "/*"):
			end, err := endBlockComment(source, i+2)
			if err != nil {
				return nil, err
			}
			i = end
			statement.WriteByte(' ')
		case source[i] == '\'' || source[i] == '"':
			end, err := endQuotedSQL(source, i, source[i])
			if err != nil {
				return nil, err
			}
			statement.WriteString(source[i:end])
			i = end
		case source[i] == '$':
			tag, ok := sqlDollarTag(source[i:])
			if !ok {
				statement.WriteByte(source[i])
				i++
				continue
			}
			end := strings.Index(source[i+len(tag):], tag)
			if end < 0 {
				return nil, errors.New("unterminated dollar-quoted string")
			}
			end = i + len(tag) + end + len(tag)
			statement.WriteString(source[i:end])
			i = end
		case source[i] == ';':
			if text := strings.TrimSpace(statement.String()); text != "" {
				statements = append(statements, text)
			}
			statement.Reset()
			i++
		default:
			statement.WriteByte(source[i])
			i++
		}
	}
	if trailing := strings.TrimSpace(statement.String()); trailing != "" {
		return nil, errors.New("unterminated SQL statement")
	}
	return statements, nil
}

func endQuotedSQL(source string, start int, quote byte) (int, error) {
	escapeString := quote == '\'' && start > 0 && (source[start-1] == 'E' || source[start-1] == 'e') && (start == 1 || !isSQLIdentifierByte(source[start-2]))
	for i := start + 1; i < len(source); i++ {
		if escapeString && source[i] == '\\' {
			i++
			continue
		}
		if source[i] != quote {
			continue
		}
		if i+1 < len(source) && source[i+1] == quote {
			i++
			continue
		}
		return i + 1, nil
	}
	return 0, errors.New("unterminated quoted string")
}

func isSQLIdentifierByte(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func endBlockComment(source string, start int) (int, error) {
	depth := 1
	for i := start; i < len(source)-1; i++ {
		switch source[i : i+2] {
		case "/*":
			depth++
			i++
		case "*/":
			depth--
			i++
			if depth == 0 {
				return i + 1, nil
			}
		}
	}
	return 0, errors.New("unterminated block comment")
}

func sqlDollarTag(source string) (string, bool) {
	end := strings.IndexByte(source[1:], '$')
	if end < 0 {
		return "", false
	}
	end++
	for i := 1; i < end; i++ {
		c := source[i]
		if !validDollarTagByte(c, i) {
			return "", false
		}
	}
	return source[:end+1], true
}

func validDollarTagByte(c byte, position int) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' || (position > 1 && c >= '0' && c <= '9')
}
