# Proxy a Smee channel to the local CodePeer webhook endpoint.
# 1. Create a channel at https://smee.io/new
# 2. Set the App webhook URL to the channel (same webhook secret)
# 3. Run: .\scripts\smee.ps1 -Channel https://smee.io/<your-channel>
param(
    [Parameter(Mandatory = $true)]
    [string]$Channel,
    [string]$Target = "http://localhost:8080/webhook"
)

$smee = "P:\ArcticWorks\tools\smee\node_modules\smee-client\bin\smee.js"
if (-not (Test-Path $smee)) {
    Write-Error "smee-client not installed. Run: npm.cmd install --prefix P:\ArcticWorks\tools\smee --cache P:\ArcticWorks\.npm-cache smee-client"
    exit 1
}
& node $smee --url $Channel --target $Target
