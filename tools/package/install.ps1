$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'agi-hub-setup.exe') @args
exit $LASTEXITCODE
