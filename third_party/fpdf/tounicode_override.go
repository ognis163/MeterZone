package fpdf

import (
	"fmt"
	"sort"
	"strings"
)

// buildToUnicodeCMap строит корректную таблицу ToUnicode для встроенного
// подмножества шрифта: каждый использованный CID (код символа Unicode)
// отображается в тот же самый Unicode. Штатная реализация fpdf пишет
// диапазон <0000>–<FFFF> с одинаковым назначением, из-за чего текст в PDF
// не копируется и не ищется.
func buildToUnicodeCMap(dict map[int]int) string {
	keys := make([]int, 0, len(dict))
	for cid := range dict {
		if cid > 0 && cid <= 0xFFFF {
			keys = append(keys, cid)
		}
	}
	sort.Ints(keys)

	var b strings.Builder
	b.WriteString("/CIDInit /ProcSet findresource begin\n")
	b.WriteString("12 dict begin\nbegincmap\n")
	b.WriteString("/CIDSystemInfo\n<</Registry (Adobe)\n/Ordering (UCS)\n/Supplement 0\n>> def\n")
	b.WriteString("/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n")
	b.WriteString("1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")

	for i := 0; i < len(keys); i += 100 {
		end := i + 100
		if end > len(keys) {
			end = len(keys)
		}
		chunk := keys[i:end]
		b.WriteString(fmt.Sprintf("%d beginbfchar\n", len(chunk)))
		for _, cid := range chunk {
			b.WriteString(fmt.Sprintf("<%04X> <%04X>\n", cid, cid))
		}
		b.WriteString("endbfchar\n")
	}

	b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend")
	return b.String()
}
