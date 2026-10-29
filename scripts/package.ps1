<#
.SYNOPSIS
  Assemble dist\Nodelight\ (portable, no installer, no admin rights)
  from the bridge and the built client (this repository's Qt app).
#>
$ErrorActionPreference = 'Stop'
$root   = Split-Path $PSScriptRoot -Parent
$deploy = Join-Path $root 'build\deploy-x64-release'
$bundle = Join-Path $root 'dist\Nodelight'

foreach ($p in @("$root\dist\Nodelight.exe", "$deploy\Moonlight.exe")) {
    if (-not (Test-Path $p)) { throw "Missing $p - build it first." }
}
if (Test-Path $bundle) { Remove-Item -Recurse -Force $bundle }
New-Item -ItemType Directory -Force $bundle | Out-Null

Copy-Item "$root\dist\Nodelight.exe" $bundle
Copy-Item -Recurse $deploy "$bundle\client"
# The build keeps the upstream file name (installer scripts depend on it); the shipped
# process is called NodelightClient.exe.
Rename-Item "$bundle\client\Moonlight.exe" "NodelightClient.exe"

# Portable mode: Moonlight keeps its settings and pairing data beside itself.
New-Item -ItemType File -Force "$bundle\client\portable.dat" | Out-Null

# App-local Visual C++ runtime, so the app runs on a PC that has never had it
# installed (moonlight-qt's deploy step deliberately leaves it out).
$crt = Get-ChildItem "${env:ProgramFiles}\Microsoft Visual Studio\*\*\VC\Redist\MSVC\*\x64\Microsoft.VC14*.CRT" -Directory -ErrorAction SilentlyContinue |
       Sort-Object FullName | Select-Object -Last 1
if ($crt) {
    Copy-Item "$($crt.FullName)\*.dll" "$bundle\client\"
    Write-Host "Bundled VC++ runtime from $($crt.FullName)"
} else {
    Write-Warning 'VC++ redistributable DLLs not found; the app will need the VC++ 2015-2022 x64 runtime installed.'
}

Copy-Item "$root\hosts.example.json" "$bundle\hosts.example.json"
Copy-Item "$root\LISEZMOI.txt" "$bundle\LISEZMOI.txt"
Copy-Item "$root\licenses" "$bundle\licenses" -Recurse

Compress-Archive -Path "$bundle\*" -DestinationPath "$root\dist\Nodelight-win-x64.zip" -Force
Write-Host "Zip: $root\dist\Nodelight-win-x64.zip"
