param(
    [Parameter(Mandatory = $true)]
    [string]$OutputPath
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

$destination = [System.IO.Path]::GetFullPath($OutputPath)
$destinationDirectory = Split-Path -Parent $destination
New-Item -ItemType Directory -Force -Path $destinationDirectory | Out-Null

function New-IconPng([int]$size) {
    $bitmap = [System.Drawing.Bitmap]::new($size, $size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    try {
        $graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
        $graphics.Clear([System.Drawing.Color]::Transparent)

        $scale = $size / 256.0
        $bounds = [System.Drawing.RectangleF]::new(12 * $scale, 12 * $scale, 232 * $scale, 232 * $scale)
        $background = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(255, 45, 40, 88))
        $graphics.FillEllipse($background, $bounds)
        $background.Dispose()

        # Six identical wings make the mark rotationally symmetric on every
        # 60-degree turn, while the center remains clear at small icon sizes.
        $center = [System.Drawing.PointF]::new(128 * $scale, 128 * $scale)
        $wingBrush = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(255, 65, 229, 208))
        for ($index = 0; $index -lt 6; $index++) {
            $angle = $index * 60 * [Math]::PI / 180
            $points = @(
                @(-10, -34),
                @(10, -34),
                @(19, -74),
                @(0, -90),
                @(-19, -74)
            ) | ForEach-Object {
                $x = $_[0] * $scale
                $y = $_[1] * $scale
                [System.Drawing.PointF]::new(
                    $center.X + ($x * [Math]::Cos($angle)) - ($y * [Math]::Sin($angle)),
                    $center.Y + ($x * [Math]::Sin($angle)) + ($y * [Math]::Cos($angle))
                )
            }
            $wing = [System.Drawing.Drawing2D.GraphicsPath]::new()
            $wing.AddPolygon([System.Drawing.PointF[]]$points)
            $graphics.FillPath($wingBrush, $wing)
            $wing.Dispose()
        }
        $wingBrush.Dispose()

        $core = [System.Drawing.Drawing2D.GraphicsPath]::new()
        for ($index = 0; $index -lt 6; $index++) {
            $angle = (-90 + ($index * 60)) * [Math]::PI / 180
            $core.AddLine(
                [System.Drawing.PointF]::new(
                    $center.X + (27 * $scale * [Math]::Cos($angle)),
                    $center.Y + (27 * $scale * [Math]::Sin($angle))
                ),
                [System.Drawing.PointF]::new(
                    $center.X + (27 * $scale * [Math]::Cos(((-90 + (($index + 1) % 6) * 60) * [Math]::PI / 180))),
                    $center.Y + (27 * $scale * [Math]::Sin(((-90 + (($index + 1) % 6) * 60) * [Math]::PI / 180)))
                )
            )
        }
        $coreBrush = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(255, 243, 250, 255))
        $graphics.FillPath($coreBrush, $core)
        $coreBrush.Dispose()
        $core.Dispose()
    } finally {
        $graphics.Dispose()
    }

    try {
        $stream = [System.IO.MemoryStream]::new()
        $bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
        # Prevent PowerShell from unrolling the PNG bytes into separate images.
        Write-Output -NoEnumerate $stream.ToArray()
    } finally {
        $bitmap.Dispose()
        if ($stream) { $stream.Dispose() }
    }
}

$sizes = @(16, 24, 32, 48, 64, 128, 256)
$images = @($sizes | ForEach-Object { New-IconPng $_ })
$file = [System.IO.File]::Open($destination, [System.IO.FileMode]::Create, [System.IO.FileAccess]::Write)
$writer = [System.IO.BinaryWriter]::new($file)
try {
    $writer.Write([UInt16]0)
    $writer.Write([UInt16]1)
    $writer.Write([UInt16]$images.Count)

    $offset = 6 + (16 * $images.Count)
    for ($index = 0; $index -lt $images.Count; $index++) {
        $size = $sizes[$index]
        $writer.Write([byte]$(if ($size -eq 256) { 0 } else { $size }))
        $writer.Write([byte]$(if ($size -eq 256) { 0 } else { $size }))
        $writer.Write([byte]0)
        $writer.Write([byte]0)
        $writer.Write([UInt16]1)
        $writer.Write([UInt16]32)
        $writer.Write([UInt32]$images[$index].Length)
        $writer.Write([UInt32]$offset)
        $offset += $images[$index].Length
    }
    foreach ($image in $images) { $writer.Write($image) }
} finally {
    $writer.Dispose()
    $file.Dispose()
}
