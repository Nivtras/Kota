# AI Usage Widget

Windows'ta AI hesaplarının kullanım/kota durumunu tek bir küçük, her zaman üstte duran pencerede gösterir. Go + Wails v2.

## Sağlayıcılar

| Sağlayıcı | Kaynak | Not |
|---|---|---|
| Claude | `~/.claude/.credentials.json` token → `api.anthropic.com/api/oauth/usage` (5 saatlik + haftalık %) | Token yoksa `~/.claude/projects/**/*.jsonl` loglarından token toplamı gösterir. Limit % için terminalde `claude` → `/login`. |
| Codex | `~/.codex/auth.json` token (sadece okunur) → `chatgpt.com/backend-api/wham/usage` | Pencere uzunluğuna göre etiket (5-hour / Weekly / Monthly). Token'ı Codex uygulaması yeniler; `~/.codex` yoksa kart gizlenir. |
| Antigravity | Çalışan `language_server.exe` → `GetUserStatus` (127.0.0.1) | Antigravity açık olmalı. Aynı kotayı paylaşan modeller gruplanır. |

İkisi de resmi olmayan endpoint'ler; güncellemeyle bozulabilir. Token'lar sadece kendi servislerine gider.

## Görünüm

- **Taskbar pill** (varsayılan): görev çubuğunda Başlat düğmesinin hemen soluna yerleşir (konum UI Automation ile bulunur, `uia_windows.go`), her sağlayıcının en çok kullanılan limitini gösterir. Panel açıkken de yerinde kalır; pencere `SetWindowRgn` ile panel + pill'e kırpılır, arada kalan taskbar tıklanabilir.
- **Tıkla → panel**: tüm limitler + Claude token detayları. Odak kaybında / Esc ile geri küçülür.
- **Ayarlar (⚙ panelde)**: taskbar'da görünecek sağlayıcılar (en fazla 2), taskbar genişliği (180–320 px; 260+ isimleri de gösterir) ve limitlerin "Used" ya da "Remaining" gösterimi. `%APPDATA%\AIUsageWidget\config.json`'a kaydedilir.

## Dağıtım

```bash
wails build -clean -skipbindings -trimpath
```

`build/bin/AIUsageWidget.exe` tek dosyadır; `packaging/KURULUM.txt` ile birlikte zip'lenip paylaşılabilir.
- Windows 11'de 3. parti taskbar widget API'si yok; pencere taskbar üstünde topmost tutulur ve tam ekran uygulamalarda gizlenir (`dock_windows.go`).

## Geliştirme

```bash
go run ./cmd/probe          # UI olmadan sağlayıcı çıktısı (JSON)
wails build -skipbindings   # build/bin/AIUsageWidget.exe
```

Yeni sağlayıcı: `providers/` altında `Provider` arayüzünü uygula, `providers.All()`'a ekle.
