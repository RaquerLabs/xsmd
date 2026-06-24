# install.ps1
$repo = "RaquerLabs/xsmd"
$installDir = "$HOME\.local\bin"
New-Item -ItemType Directory -Force -Path $installDir | Out-Null

# Get latest release tag
$latestTag = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest").tag_name

# Detect Architecture
$arch = if ([Environment]::Is64BitOperatingSystem) { "amd64" } else { "386" }
$fileName = "xsmd-lsp-$latestTag-windows-$arch.zip"
$url = "https://github.com/$repo/releases/download/$latestTag/$fileName"

echo "Downloading $url..."
Invoke-WebRequest -Uri $url -OutFile "$env:TEMP\$fileName"

echo "Extracting to $installDir..."
Expand-Archive -Path "$env:TEMP\$fileName" -DestinationPath $installDir -Force

echo "Done! Ensure $installDir is in your PATH."
