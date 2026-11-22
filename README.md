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

- **Taskbar pill**: Başlat düğmesinin hemen solunda, her sağlayıcının 5 saatlik limitini (yoksa en çok kullanılan limiti) gösterir.
- **Tıkla → panel**: tüm limitler + token detayları. Pill'e tekrar tıklayınca, dışarı tıklayınca, Esc ile ya da odak kaybında kapanır.
- **Ayarlar (⚙ panelde)**: taskbar'da görünecek sağlayıcılar (en fazla 2), taskbar genişliği (180–320 px; 260+ isimleri de gösterir) ve limitlerin "Used" ya da "Remaining" gösterimi. `%APPDATA%\AIUsageWidget\config.json`'a kaydedilir.

- **Hız işareti**: barlardaki ince çizgi pencerenin zamanının ne kadarının geçtiğini gösterir; kullanım çizgiyi geçtiyse saatten hızlı harcıyorsun. Bu hızla sıfırlanmadan önce limit dolacaksa panelde "At this pace: limit reached in …" uyarısı çıkar.
- **Bildirimler**: limit uyarı (varsayılan %80) ve kritik (varsayılan %95) eşiğine gelince (pencere başına bir kez) ve dolmuş bir limit yeniden kullanılabilir olunca Windows bildirimi. Eşikler ayarlardan değişir, kapatılabilir; ek istek atmaz. Tam ekran / sunum / oyun sırasında bekletilir, bitince gösterilir.
- **Tooltip ve sağ tık**: pill'in üstünde durunca tüm limitler tooltip'te; sağ tık menüsü: Open panel, Refresh now, Settings…, Reset position, Quit.
- **Açık/koyu tema**: pill ve panel Windows'un taskbar temasını izler (test için `AIW_THEME=light|dark`).
- **Uyarlanır yenileme**: aktifken 2 dk, 10 dk'dan uzun boştayken 15 dk; bir limit sıfırlanınca 30 sn sonra, panel açılınca veri 1 dk'dan eskiyse ve boştan dönünce hemen.
- **Kullandığım aracı öne al** (ayar): öndeki uygulama Claude / Codex / Antigravity ise (terminal ve editör içindeki CLI'lar dahil) pill'de o sağlayıcı önce gösterilir.
- **Kullanım geçmişi**: kart açılınca son 7 günün grafiği (pill'deki limit + en uzun limit), üstüne gelince o anki değerler. Çekilen veriden örneklenir (`history.json`), ek istek atmaz.
- **Tüm monitörlerde göster** (ayar): her ikincil taskbar'a (`Shell_SecondaryTrayWnd`) da bir pill; panel tıklanan pill'in monitöründe açılır.

## Nasıl çalışıyor

Windows 11'de üçüncü parti taskbar widget API'si yok (hava durumu widget'ı Explorer'ın kendi parçası). Bu yüzden:

| Parça | Dosya | Yaklaşım |
|---|---|---|
| Pill | `pill_windows.go` | GDI ile çizilen native pencere, `Shell_TrayWnd`'nin child'ı. `WS_EX_LAYERED` olmalı (Win11 taskbar DirectComposition tabanlı, düz GDI child görünmez). Native pencere olduğu için tıklama/hover doğrudan gelir. |
| Konum | `uia_windows.go`, `pill_windows.go` | Başlat'ın yeri UI Automation (COM) ile bulunur; taskbar'daki değişiklikler `SetWinEventHook` ile anında yakalanır, 5 sn'lik yedek zamanlayıcı da var. UIA çağrısı pencere thread'inde yapılmaz (Explorer ile karşılıklı beklemeye giriyordu). |
| Explorer yeniden başlarsa | `pill_windows.go` | Explorer'ın yayınladığı `TaskbarCreated` mesajıyla pill yeniden oluşturulur. |
| Panel | `panel_windows.go` | Wails penceresi, normal topmost popup; göster/gizle. Hiç re-parent edilmez. |
| Veri | `app.go`, `refresh.go` | Uyarlanır aralıkla çeker, pill'i günceller ve panele `usage` event'i yollar. |
| Pill içeriği | `pill_model.go` | Hangi limitin gösterileceği, renk, sıfırlanma metni (test: `pill_model_test.go`). |
| Çoklu monitör | `pills_windows.go` | Ana pill + ikincil taskbar başına bir pill; 3 sn'de bir eşitlenir. |
| Kurulum | `install_windows.go` | Exe kendini `%LOCALAPPDATA%\Programs\AIUsageWidget`'a kopyalar, Başlat menüsü kısayolu (IShellLink) ve Ayarlar → Uygulamalar kaydı ekler; yönetici izni yok. Yeni exe çalıştırılınca günceller. `--uninstall`, `--portable`. |
| Geçmiş | `history.go` | Limit başına 10 dk'da bir (değer ≥1 puan değişirse hemen) örnek, 7 gün saklanır. |
| Aktif araç | `active_windows.go` | Öndeki pencerenin exe'si; terminal/editörse süreç ağacında CLI aranır. |
| Claude token | `providers/claude.go` | Süresi dolunca yenilenir ve dosyaya geri yazılır (diğer alanlar aynen korunur). Claude Code aynı anda yenilediyse dosyadaki yeni token kullanılır. |

## Dağıtım

```bash
wails build -clean -skipbindings -trimpath
```

`build/bin/AIUsageWidget.exe` tek dosyadır; `packaging/KURULUM.txt` ile birlikte zip'lenip paylaşılabilir (`release/AIUsageWidget-win64.zip`). İlk çalıştırmada kurulum sorar. Sürüm: `install_windows.go` içindeki `appVersion` ve `wails.json`.

## Geliştirme

```bash
go run ./cmd/probe          # UI olmadan sağlayıcı çıktısı (JSON)
wails build -skipbindings   # build/bin/AIUsageWidget.exe
```

Yeni sağlayıcı: `providers/` altında `Provider` arayüzünü uygula, `providers.All()`'a ekle.
