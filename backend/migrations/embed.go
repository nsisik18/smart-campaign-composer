// Package migrations SQL migration dosyalarını binary'ye gömer; migrate
// servisi container içinde dosya sistemine bağımlı olmadan çalışır.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
