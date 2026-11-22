# Renders the Kota logo (same geometry as build/logo.svg) to build/appicon.png
# (1024 px, Wails source icon) and build/windows/icon.ico (16-256 px).
# Usage: pwsh build/makeicon.ps1
Add-Type -AssemblyName System.Drawing
$root = Split-Path -Parent $MyInvocation.MyCommand.Path

function RoundRect([float]$x, [float]$y, [float]$w, [float]$h, [float]$r) {
  $p = New-Object System.Drawing.Drawing2D.GraphicsPath
  $d = 2 * $r
  $p.AddArc($x, $y, $d, $d, 180, 90)
  $p.AddArc($x + $w - $d, $y, $d, $d, 270, 90)
  $p.AddArc($x + $w - $d, $y + $h - $d, $d, $d, 0, 90)
  $p.AddArc($x, $y + $h - $d, $d, $d, 90, 90)
  $p.CloseFigure(); $p
}
function C([string]$hex) { [System.Drawing.ColorTranslator]::FromHtml($hex) }

function Render([int]$size) {
  $bmp = New-Object System.Drawing.Bitmap $size, $size, ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.SmoothingMode = 'AntiAlias'; $g.PixelOffsetMode = 'HighQuality'; $g.Clear([System.Drawing.Color]::Transparent)
  $s = $size / 1024.0
  $g.ScaleTransform($s, $s)
  # small sizes: thicker shapes so they survive downscaling
  $boost = if ($size -le 24) { 1.25 } elseif ($size -le 48) { 1.1 } else { 1.0 }
  $tile = New-Object System.Drawing.Drawing2D.LinearGradientBrush ([System.Drawing.PointF]::new(0, 32)), ([System.Drawing.PointF]::new(0, 992)), (C '#262A33'), (C '#14161B')
  $g.FillPath($tile, (RoundRect 32 32 960 960 224))
  # two usage bars (like the taskbar pill) and the pace marker through both
  $bh = 128 * $boost; $gap = 72
  $tracks = (C '#3A3F4B')
  foreach ($row in @(@{ y = 512 - $gap / 2 - $bh; w = 470; a = '#2FC977'; b = '#5BE39A' },
                     @{ y = 512 + $gap / 2;       w = 270; a = '#E9A81F'; b = '#F5C451' })) {
    $g.FillPath((New-Object System.Drawing.SolidBrush $tracks), (RoundRect 176 $row.y 672 $bh ($bh / 2)))
    $br = New-Object System.Drawing.Drawing2D.LinearGradientBrush ([System.Drawing.PointF]::new(176, 0)), ([System.Drawing.PointF]::new(176 + $row.w, 0)), (C $row.a), (C $row.b)
    $g.FillPath($br, (RoundRect 176 $row.y $row.w $bh ($bh / 2)))
  }
  $mw = 52 * $boost; $mh = 2 * $bh + $gap + 96
  $g.FillPath((New-Object System.Drawing.SolidBrush (C '#F2F3F5')), (RoundRect (712 - $mw / 2) (512 - $mh / 2) $mw $mh ($mw / 2)))
  $g.Dispose(); $bmp
}

$big = Render 1024
$big.Save((Join-Path $root 'appicon.png'), [System.Drawing.Imaging.ImageFormat]::Png)

# ICO with PNG-compressed entries
$sizes = 16, 20, 24, 32, 40, 48, 64, 256
$pngs = foreach ($sz in $sizes) {
  $ms = New-Object System.IO.MemoryStream
  (Render $sz).Save($ms, [System.Drawing.Imaging.ImageFormat]::Png); , $ms.ToArray()
}
$out = New-Object System.IO.MemoryStream
$w = New-Object System.IO.BinaryWriter $out
$w.Write([uint16]0); $w.Write([uint16]1); $w.Write([uint16]$sizes.Count)
$offset = 6 + 16 * $sizes.Count
for ($i = 0; $i -lt $sizes.Count; $i++) {
  $sz = $sizes[$i]; $b = if ($sz -ge 256) { 0 } else { $sz }
  $w.Write([byte]$b); $w.Write([byte]$b); $w.Write([byte]0); $w.Write([byte]0)
  $w.Write([uint16]1); $w.Write([uint16]32); $w.Write([uint32]$pngs[$i].Length); $w.Write([uint32]$offset)
  $offset += $pngs[$i].Length
}
foreach ($p in $pngs) { $w.Write($p) }
[System.IO.File]::WriteAllBytes((Join-Path $root 'windows\icon.ico'), $out.ToArray())
# preview strip for checking small sizes
$strip = New-Object System.Drawing.Bitmap 420, 140
$sg = [System.Drawing.Graphics]::FromImage($strip); $sg.Clear((C '#F3F3F3'))
$x = 10; foreach ($sz in 16, 24, 32, 48, 128) { $sg.DrawImage((Render $sz), $x, 10, $sz, $sz); $x += $sz + 14 }
$sg.FillRectangle((New-Object System.Drawing.SolidBrush (C '#202020')), 0, 70, 420, 70)
$x = 10; foreach ($sz in 16, 24, 32, 48) { $sg.DrawImage((Render $sz), $x, 80, $sz, $sz); $x += $sz + 14 }
$strip.Save("$env:TEMP\claude\kota_icon_preview.png")
"ok"
