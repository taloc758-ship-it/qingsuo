param(
    [string]$OutputDirectory = (Join-Path $PSScriptRoot "..\assets\icons")
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null

function New-Brush([int[]]$rgb) {
    [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(255, $rgb[0], $rgb[1], $rgb[2]))
}

function Fill-RoundedRectangle([System.Drawing.Graphics]$g, [System.Drawing.Brush]$brush, [System.Drawing.RectangleF]$rect, [float]$radius) {
    $path = [System.Drawing.Drawing2D.GraphicsPath]::new()
    try {
        $path.AddArc($rect.X, $rect.Y, $radius, $radius, 180, 90)
        $path.AddArc($rect.Right - $radius, $rect.Y, $radius, $radius, 270, 90)
        $path.AddArc($rect.Right - $radius, $rect.Bottom - $radius, $radius, $radius, 0, 90)
        $path.AddArc($rect.X, $rect.Bottom - $radius, $radius, $radius, 90, 90)
        $path.CloseFigure()
        $g.FillPath($brush, $path)
    } finally { $path.Dispose() }
}

function Draw-Variant([System.Drawing.Graphics]$g, [string]$variant, [int]$size) {
    $s = $size / 256.0
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
    $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
    $g.Clear([System.Drawing.Color]::Transparent)
    $rect = [System.Drawing.RectangleF]::new(4*$s, 4*$s, 248*$s, 248*$s)
    $background = switch ($variant) {
        "orbit" { New-Brush @(36, 27, 85) }
        "shield" { New-Brush @(20, 63, 112) }
        "prism" { New-Brush @(76, 35, 112) }
        "pulse" { New-Brush @(76, 24, 65) }
        default { New-Brush @(20, 86, 86) }
    }
    Fill-RoundedRectangle $g $background $rect (42*$s)
    $background.Dispose()

    $penWidth = [Math]::Max(2.0, 14*$s)
    $center = [System.Drawing.PointF]::new(128*$s,128*$s)
    switch ($variant) {
        "orbit" {
            $ring = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(255, 107, 239, 229), $penWidth)
            $g.DrawEllipse($ring, [System.Drawing.RectangleF]::new(35*$s, 58*$s, 186*$s, 140*$s))
            $ring.Dispose()
            $core = New-Brush @(245,250,255)
            $g.FillEllipse($core, [System.Drawing.RectangleF]::new(94*$s,94*$s,68*$s,68*$s))
            $core.Dispose()
            $dot = New-Brush @(255, 205, 90)
            $g.FillEllipse($dot, [System.Drawing.RectangleF]::new(196*$s,111*$s,25*$s,25*$s))
            $dot.Dispose()
        }
        "shield" {
            $path = [System.Drawing.Drawing2D.GraphicsPath]::new()
            $path.AddPolygon([System.Drawing.PointF[]]@(
                [System.Drawing.PointF]::new(128*$s, 25*$s), [System.Drawing.PointF]::new(209*$s, 56*$s),
                [System.Drawing.PointF]::new(198*$s, 144*$s), [System.Drawing.PointF]::new(128*$s, 226*$s),
                [System.Drawing.PointF]::new(58*$s, 144*$s), [System.Drawing.PointF]::new(47*$s, 56*$s)))
            $shield = New-Brush @(97, 226, 255)
            $g.FillPath($shield, $path); $shield.Dispose(); $path.Dispose()
            $check = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(255, 15, 47, 82), 18*$s)
            $check.StartCap = [System.Drawing.Drawing2D.LineCap]::Round; $check.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
            $g.DrawLines($check, [System.Drawing.PointF[]]@([System.Drawing.PointF]::new(84*$s,126*$s), [System.Drawing.PointF]::new(113*$s,155*$s), [System.Drawing.PointF]::new(174*$s,94*$s)))
            $check.Dispose()
        }
        "prism" {
            $p = [System.Drawing.PointF[]]@([System.Drawing.PointF]::new(128*$s,24*$s), [System.Drawing.PointF]::new(225*$s,128*$s), [System.Drawing.PointF]::new(128*$s,232*$s), [System.Drawing.PointF]::new(31*$s,128*$s))
            $left = New-Brush @(255, 115, 198); $right = New-Brush @(96, 230, 255); $top = New-Brush @(255, 221, 112)
            $g.FillPolygon($left, [System.Drawing.PointF[]]@($p[0],$p[1],$p[2],$center)); $g.FillPolygon($right, [System.Drawing.PointF[]]@($p[0],$center,$p[2],$p[3])); $g.FillPolygon($top, [System.Drawing.PointF[]]@($p[0],$p[1],$center,$p[3]))
            $left.Dispose(); $right.Dispose(); $top.Dispose()
            $edge = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(255,255,255,255), 7*$s); $edge.LineJoin = [System.Drawing.Drawing2D.LineJoin]::Round
            $g.DrawPolygon($edge,$p); $edge.Dispose()
        }
        "pulse" {
            $circle = New-Brush @(17, 22, 54); $g.FillEllipse($circle, [System.Drawing.RectangleF]::new(17*$s,17*$s,222*$s,222*$s)); $circle.Dispose()
            $wave = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(255, 255, 104, 190), 16*$s); $wave.StartCap = [System.Drawing.Drawing2D.LineCap]::Round; $wave.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
            $g.DrawLines($wave, [System.Drawing.PointF[]]@([System.Drawing.PointF]::new(31*$s,139*$s),[System.Drawing.PointF]::new(72*$s,139*$s),[System.Drawing.PointF]::new(94*$s,81*$s),[System.Drawing.PointF]::new(120*$s,177*$s),[System.Drawing.PointF]::new(147*$s,109*$s),[System.Drawing.PointF]::new(170*$s,139*$s),[System.Drawing.PointF]::new(225*$s,139*$s))); $wave.Dispose()
            $spark = New-Brush @(255, 223, 114); $g.FillEllipse($spark,[System.Drawing.RectangleF]::new(185*$s,43*$s,31*$s,31*$s)); $spark.Dispose()
        }
        default {
            $ringPen = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(255, 255, 194, 92), 20*$s); $ringPen.StartCap = [System.Drawing.Drawing2D.LineCap]::Round; $ringPen.EndCap = [System.Drawing.Drawing2D.LineCap]::Round
            $g.DrawArc($ringPen, [System.Drawing.RectangleF]::new(39*$s,39*$s,178*$s,178*$s), -55, 215); $g.DrawArc($ringPen, [System.Drawing.RectangleF]::new(39*$s,39*$s,178*$s,178*$s), 125, 215); $ringPen.Dispose()
            $core = New-Brush @(230, 255, 235); $g.FillEllipse($core,[System.Drawing.RectangleF]::new(94*$s,94*$s,68*$s,68*$s)); $core.Dispose()
        }
    }
}

$sizes = @(16,24,32,48,64,128,256)
$variants = @("orbit","shield","prism","pulse","knot")
foreach ($variant in $variants) {
    $pngs = @{}
    foreach ($size in $sizes) {
        $bitmap = [System.Drawing.Bitmap]::new($size,$size,[System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
        $g = [System.Drawing.Graphics]::FromImage($bitmap)
        try { Draw-Variant $g $variant $size; $stream=[System.IO.MemoryStream]::new(); $bitmap.Save($stream,[System.Drawing.Imaging.ImageFormat]::Png); $pngs[$size]=$stream.ToArray(); if ($size -eq 256) { [System.IO.File]::WriteAllBytes((Join-Path $OutputDirectory "qingsuo-$variant.png"),$pngs[$size]) } } finally { if ($stream) {$stream.Dispose()}; $g.Dispose(); $bitmap.Dispose() }
    }
    $icoPath = Join-Path $OutputDirectory "qingsuo-$variant.ico"
    $file=[System.IO.File]::Open($icoPath,[System.IO.FileMode]::Create); $writer=[System.IO.BinaryWriter]::new($file)
    try {
        $writer.Write([UInt16]0); $writer.Write([UInt16]1); $writer.Write([UInt16]$sizes.Count); $offset=6+(16*$sizes.Count)
        foreach ($size in $sizes) { $bytes=$pngs[$size]; $writer.Write([byte]$(if($size -eq 256){0}else{$size})); $writer.Write([byte]$(if($size -eq 256){0}else{$size})); $writer.Write([byte]0); $writer.Write([byte]0); $writer.Write([UInt16]1); $writer.Write([UInt16]32); $writer.Write([UInt32]$bytes.Length); $writer.Write([UInt32]$offset); $offset += $bytes.Length }
        foreach ($size in $sizes) { $writer.Write($pngs[$size]) }
    } finally { $writer.Dispose(); $file.Dispose() }
}
Write-Output "Generated $($variants.Count) icon variants in $OutputDirectory"
