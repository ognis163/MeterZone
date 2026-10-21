package docgen

import (
	"bytes"
	"fmt"
	"strings"
)

// pdfTitleMarker — временная ASCII-строка в Info-словаре PDF. После сборки
// документа мы заменяем её настоящим названием в UTF-16BE.
const pdfTitleMarker = "MZPDFTITLE"

// patchPDFTitle заменяет /Title (MZPDFTITLE) на /Title <FEFF…>.
//
// Метаданные PDF по стандарту (PDF 32000, 7.9.2) хранят не-ASCII строки
// в UTF-16BE с BOM. gpdf пишет значение литеральной строкой, и кириллица
// там ломается: браузер показывает на вкладке иероглифы вместо названия.
// HEX-строка <FEFF…> разбирается однозначно любым просмотрщиком.
func patchPDFTitle(data []byte, title string) []byte {
	title = strings.TrimSpace(title)
	if title == "" {
		return data
	}
	var hex strings.Builder
	hex.WriteString("/Title <FEFF")
	for _, r := range title {
		if r > 0xFFFF {
			r = '?'
		}
		hex.WriteString(fmt.Sprintf("%04X", r))
	}
	hex.WriteString(">")

	old := []byte("/Title (" + pdfTitleMarker + ")")
	if !bytes.Contains(data, old) {
		return data
	}
	return bytes.Replace(data, old, []byte(hex.String()), 1)
}
