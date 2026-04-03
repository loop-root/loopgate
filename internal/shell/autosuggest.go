package shell

import (
	"strings"

	"github.com/chzyer/readline"
)

type completionOperation interface {
	EnterCompleteMode(offset int, candidate [][]rune)
	ExitCompleteMode(revent bool)
	IsInCompleteMode() bool
}

func MaybeTriggerSlashSuggestions(operation completionOperation, completer *MorphCompleter, currentLine []rune, currentPos int, key rune) {
	if operation == nil || completer == nil {
		return
	}

	predictedLine, predictedPos, predicted := previewEditedLine(currentLine, currentPos, key)
	if !predicted {
		return
	}

	commandToken, showSuggestions := slashCommandToken(string(predictedLine[:predictedPos]))
	if !showSuggestions {
		if operation.IsInCompleteMode() {
			operation.ExitCompleteMode(false)
		}
		return
	}

	commandSuffixes := completer.CommandSuffixes(commandToken)
	if len(commandSuffixes) == 0 {
		if operation.IsInCompleteMode() {
			operation.ExitCompleteMode(false)
		}
		return
	}

	operation.EnterCompleteMode(0, commandSuffixes)
}

func previewEditedLine(currentLine []rune, currentPos int, key rune) ([]rune, int, bool) {
	switch {
	case key >= 32 && key != readline.CharBackspace:
		insertedLine := make([]rune, 0, len(currentLine)+1)
		insertedLine = append(insertedLine, currentLine[:currentPos]...)
		insertedLine = append(insertedLine, key)
		insertedLine = append(insertedLine, currentLine[currentPos:]...)
		return insertedLine, currentPos + 1, true
	case key == readline.CharBackspace || key == readline.CharCtrlH:
		if currentPos <= 0 {
			return currentLine, currentPos, true
		}
		deletedLine := make([]rune, 0, len(currentLine)-1)
		deletedLine = append(deletedLine, currentLine[:currentPos-1]...)
		deletedLine = append(deletedLine, currentLine[currentPos:]...)
		return deletedLine, currentPos - 1, true
	case key == readline.CharDelete:
		if currentPos >= len(currentLine) {
			return currentLine, currentPos, true
		}
		deletedLine := make([]rune, 0, len(currentLine)-1)
		deletedLine = append(deletedLine, currentLine[:currentPos]...)
		deletedLine = append(deletedLine, currentLine[currentPos+1:]...)
		return deletedLine, currentPos, true
	case key == readline.CharCtrlU:
		return nil, 0, true
	default:
		return currentLine, currentPos, false
	}
}

func slashCommandToken(linePrefix string) (string, bool) {
	trimmedPrefix := strings.TrimLeft(linePrefix, " ")
	if !strings.HasPrefix(trimmedPrefix, "/") {
		return "", false
	}

	if strings.ContainsAny(trimmedPrefix, " \t") {
		fields := strings.Fields(trimmedPrefix)
		if len(fields) != 1 {
			return "", false
		}
		if strings.HasSuffix(trimmedPrefix, " ") || strings.HasSuffix(trimmedPrefix, "\t") {
			return "", false
		}
		return fields[0], true
	}

	return trimmedPrefix, true
}
