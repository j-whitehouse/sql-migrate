package sqlparse

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"strings"
)

const (
	sqlCmdPrefix        = "-- +migrate "
	optionNoTransaction = "notransaction"
)

type migrationStatement struct {
	Statement   string
	Loop        bool
	Conditional string
}

type ParsedMigration struct {
	// Statements need a simple "Statement" string, a "Loop" flag and a loop "Conditional" string
	UpStatements   []migrationStatement
	DownStatements []migrationStatement

	DisableTransactionUp   bool
	DisableTransactionDown bool
}

var (
	// LineSeparator can be used to split migrations by an exact line match. This line
	// will be removed from the output. If left blank, it is not considered. It is defaulted
	// to blank so you will have to set it manually.
	// Use case: in MSSQL, it is convenient to separate commands by GO statements like in
	// SQL Query Analyzer.
	LineSeparator = ""
)

func errNoTerminator() error {
	if len(LineSeparator) == 0 {
		return errors.New(`ERROR: The last statement must be ended by a semicolon or '-- +migrate StatementEnd' marker.
			See https://github.com/j-whitehouse/sql-migrate for details.`)
	}

	return errors.New(fmt.Sprintf(`ERROR: The last statement must be ended by a semicolon, a line whose contents are %q, or '-- +migrate StatementEnd' marker.
			See https://github.com/j-whitehouse/sql-migrate for details.`, LineSeparator))
}

// Checks the line to see if the line has a statement-ending semicolon
// or if the line contains a double-dash comment.
func endsWithSemicolon(line string) bool {

	prev := ""
	scanner := bufio.NewScanner(strings.NewReader(line))
	scanner.Split(bufio.ScanWords)

	for scanner.Scan() {
		word := scanner.Text()
		if strings.HasPrefix(word, "--") {
			break
		}
		prev = word
	}

	return strings.HasSuffix(prev, ";")
}

type migrationDirection int

const (
	directionNone migrationDirection = iota
	directionUp
	directionDown
)

type migrateCommand struct {
	Command string
	Options []string
}

func (c *migrateCommand) HasOption(opt string) bool {
	for _, specifiedOption := range c.Options {
		if specifiedOption == opt {
			return true
		}
	}

	return false
}

func parseCommand(line string) (*migrateCommand, error) {
	cmd := &migrateCommand{}

	if !strings.HasPrefix(line, sqlCmdPrefix) {
		return nil, errors.New("ERROR: not a sql-migrate command")
	}

	fields := strings.Fields(line[len(sqlCmdPrefix):])
	if len(fields) == 0 {
		return nil, errors.New(`ERROR: incomplete migration command`)
	}

	cmd.Command = fields[0]

	cmd.Options = fields[1:]

	return cmd, nil
}

// ParseMigration will split the given sql script into individual statements.
// This is where the extended vocabulary of the migrations is defined
//
// The base case is to simply split on semicolons, as these
// naturally terminate a statement.
//
// However, more complex cases like pl/pgsql can have semicolons
// within a statement. For these cases, we provide the explicit annotations
// 'StatementBegin' and 'StatementEnd' to allow the script to
// tell us to ignore semicolons.
func ParseMigration(r io.ReadSeeker) (*ParsedMigration, error) {
	p := &ParsedMigration{}

	_, err := r.Seek(0, 0)
	if err != nil {
		return nil, err
	}

	// new migrationStatement type requires both
	var statementBuf bytes.Buffer
	var conditionalBuf bytes.Buffer
	scanner := bufio.NewScanner(r)

	statementEnded := false
	ignoreSemicolons := false
	currentDirection := directionUp // For flyway script compatibility :-(
	isLoop := false
	isConditional := false

	for scanner.Scan() {
		line := scanner.Text()
		// ignore comment except beginning with '-- +'
		if strings.HasPrefix(line, "-- ") && !strings.HasPrefix(line, "-- +") {
			continue
		}

		// handle any migrate-specific commands
		if strings.HasPrefix(line, sqlCmdPrefix) {
			cmd, err := parseCommand(line)
			if err != nil {
				return nil, err
			}

			switch cmd.Command {
			case "Up":
				if len(strings.TrimSpace(statementBuf.String())) > 0 {
					return nil, errNoTerminator()
				}
				currentDirection = directionUp
				if cmd.HasOption(optionNoTransaction) {
					p.DisableTransactionUp = true
				}
				break

			case "Down":
				if len(strings.TrimSpace(statementBuf.String())) > 0 {
					return nil, errNoTerminator()
				}
				currentDirection = directionDown
				if cmd.HasOption(optionNoTransaction) {
					p.DisableTransactionDown = true
				}
				break

			case "StatementBegin":
				if isLoop || isConditional {
					return nil, errors.New("ERROR: Cannot begin a statement block inside a loop or conditional block")
				}
				if currentDirection != directionNone {
					ignoreSemicolons = true
				}
				break

			case "StatementEnd":
				if isLoop || isConditional {
					break
				}
				if currentDirection != directionNone {
					statementEnded = (ignoreSemicolons == true)
					ignoreSemicolons = false
				}
				break

			case "ConditionalBegin":
				if !isLoop {
					return nil, errors.New("ERROR: saw '-- +migration ConditionalBegin' outside of matching '-- +migrate LoopBegin'")
				}
				isConditional = true
			case "ConditionalEnd":
				// don't care if ends without beginning
				isConditional = false
			case "LoopBegin":
				if ignoreSemicolons {
					return nil, errors.New("ERROR: Cannot begin a loop inside another loop or statement block")
				}
				if currentDirection != directionNone {
					isLoop = true
					ignoreSemicolons = true // Piggy backing on same logic as statements
					if currentDirection == directionUp {
						p.DisableTransactionUp = true
					} else if currentDirection == directionDown {
						p.DisableTransactionDown = true
					}
				}

				break
			case "LoopEnd":
				if isConditional {
					return nil, errors.New("ERROR: saw '-- +migrate ConditionalBegin' with no matching '-- +migrate ConditionalEnd' inside its loop")
				}
				// Need to avoid _ending_ a loop outside loops as we could mess up statement blocks
				if !isLoop {
					break
				}
				if currentDirection != directionNone {
					// Piggy backing on same logic as statements
					statementEnded = (ignoreSemicolons == true)
					ignoreSemicolons = false
				}
				break
			}
		}

		if currentDirection == directionNone {
			continue
		}

		isLineSeparator := !ignoreSemicolons && len(LineSeparator) > 0 && line == LineSeparator

		// Append additional query text to query buffer
		// Not worried about isConditional, as only true with isLoop
		if !isLineSeparator && !strings.HasPrefix(line, "-- +") && !isLoop {
			if _, err := statementBuf.WriteString(line + "\n"); err != nil {
				return nil, err
			}
		}

		// Inside a loop need to pick query v. conditional text
		if isLoop {
			if isConditional {
				if _, err := conditionalBuf.WriteString(line + "\n"); err != nil {
					return nil, err
				}
			} else {
				if _, err := statementBuf.WriteString(line + "\n"); err != nil {
					return nil, err
				}
			}
		}

		// Wrap up the two supported cases: 1) basic with semicolon; 2) psql statement
		// Lines that end with semicolon that are in a statement block
		// do not conclude statement.
		/* Conditional statement block must exist as one-per-loop, and be a single query
		That query must return 0 for finished and and int > 0 for not-finished
		Loop is any SQL statements outside the conditional block (conditional can sit anywhere)
		The Loop & Conditional need to be part of the same migrationStatement
		*/
		if (!ignoreSemicolons && (endsWithSemicolon(line) || isLineSeparator)) || statementEnded {
			statementEnded = false
			switch currentDirection {
			case directionUp:
				newStatement := migrationStatement{statementBuf.String(), isLoop, conditionalBuf.String()}
				p.UpStatements = append(p.UpStatements, newStatement)

			case directionDown:
				newStatement := migrationStatement{statementBuf.String(), isLoop, conditionalBuf.String()}
				p.DownStatements = append(p.DownStatements, newStatement)

			default:
				panic("impossible state")
			}

			isLoop = false
			statementBuf.Reset()
			conditionalBuf.Reset()
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// diagnose likely migration script errors
	if ignoreSemicolons {
		return nil, errors.New("ERROR: saw '-- +migrate StatementBegin' with no matching '-- +migrate StatementEnd'")
	}

	// validate no unclosed loop
	if isLoop {
		return nil, errors.New("ERROR: saw '-- +migrate LoopBegin' with no matching '-- +migrate LoopEnd'")
	}

	// validate no unclosed conditional
	if isConditional {
		return nil, errors.New("ERROR: saw '-- +migrate ConditionalBegin' with no matching '-- +migrate ConditionalEnd'")
	}

	if currentDirection == directionNone {
		return nil, errors.New(`ERROR: no Up/Down annotations found, so no statements were executed.
			See https://github.com/j-whitehouse/sql-migrate for details`)
	}

	// allow comment without sql instruction. Example:
	// -- +migrate Down
	// -- nothing to downgrade!
	if len(strings.TrimSpace(statementBuf.String())) > 0 && !strings.HasPrefix(statementBuf.String(), "-- +") {
		return nil, errNoTerminator()
	}

	return p, nil
}
