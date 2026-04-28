package main

import (
	"fmt"
	"html/template"
	"path/filepath"
	"slices"
	"strings"
)

// GetTemplates returns the parsed templates with custom functions.
func GetTemplates() *template.Template {
	return template.Must(template.New("").Funcs(template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
		"mul": func(a, b int) int { return a * b },
		"mod": func(a, b int) int { return a % b },
		"min": func(a, b int) int {
			if a < b {
				return a
			}
			return b
		},
		"max": func(a, b int) int {
			if a > b {
				return a
			}
			return b
		},
		"basename": func(path string) string {
			base := filepath.Base(path)
			if i := strings.LastIndexByte(base, '.'); i >= 0 {
				return base[:i]
			}
			return base
		},
		"intRange": func(start, end int) []int {
			var result []int
			for i := start; i <= end; i++ {
				result = append(result, i)
			}
			return result
		},
		"headerLink": func(label, field string, p *Page) template.HTML {
			nextOrder := "ASC"
			arrow := ""
			if p.OrderBy == field {
				if strings.ToUpper(p.Order) == "ASC" {
					nextOrder = "DESC"
					arrow = "▲"
				} else {
					arrow = "▼"
				}
			}

			url := fmt.Sprintf("./?pageSize=%d&Page=%d%s", p.PageSize, p.CurrentPage, pageLink(p, field, nextOrder))
			return template.HTML(fmt.Sprintf(`<a href="%s">%s%s</a>`, url, label, arrow))
		},
		"pageLink": func(p *Page) template.URL {
			return pageLink(p, p.OrderBy, p.Order)
		},
		"columnVisibility": func(dropList []string, name string) template.CSS {
			if slices.Contains(dropList, name) {
				return template.CSS("visibility: collapse")
			}
			return template.CSS("visibility: visible")
		},
	}).ParseFiles("templates/summary.html"))
}
