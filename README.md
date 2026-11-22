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
- **Bildirimler**: limit %80 ve %95'e gelince (pencere başına bir kez) ve dolmuş bir limit yeniden kullanılabilir olunca Windows bildirimi. Ayarlardan kapatılabilir; ek istek atmaz.

## Nasıl çalışıyor

Windows 11'de üçüncü parti taskbar widget API'si yok (hava durumu widget'ı Explorer'ın kendi parçası). Bu yüzden:

| Parça | Dosya | Yaklaşım |
|---|---|---|
| Pill | `pill_windows.go` | GDI ile çizilen native pencere, `Shell_TrayWnd`'nin child'ı. `WS_EX_LAYERED` olmalı (Win11 taskbar DirectComposition tabanlı, düz GDI child görünmez). Native pencere olduğu için tıklama/hover doğrudan gelir. |
| Konum | `uia_windows.go`, `pill_windows.go` | Başlat'ın yeri UI Automation (COM) ile bulunur; taskbar'daki değişiklikler `SetWinEventHook` ile anında yakalanır, 5 sn'lik yedek zamanlayıcı da var. UIA çağrısı pencere thread'inde yapılmaz (Explorer ile karşılıklı beklemeye giriyordu). |
| Explorer yeniden başlarsa | `pill_windows.go` | Explorer'ın yayınladığı `TaskbarCreated` mesajıyla pill yeniden oluşturulur. |
| Panel | `panel_windows.go` | Wails penceresi, normal topmost popup; göster/gizle. Hiç re-parent edilmez. |
| Veri | `app.go` | Go 2 dakikada bir çeker, pill'i günceller ve panele `usage` event'i yollar. |
| Pill içeriği | `pill_model.go` | Hangi limitin gösterileceği, renk, sıfırlanma metni (test: `pill_model_test.go`). |
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
