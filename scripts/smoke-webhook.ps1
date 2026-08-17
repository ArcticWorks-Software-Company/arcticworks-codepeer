# Send a locally-signed GitHub-style webhook to a running CodePeer instance.
# Usage: .\scripts\smoke-webhook.ps1 -Secret <webhook-secret> [-Url http://localhost:8080/webhook] [-Event pull_request] [-Delivery <guid>] [-BodyFile <path>]
param(
    [Parameter(Mandatory = $true)]
    [string]$Secret,
    [string]$Url = "http://localhost:8080/webhook",
    [string]$Event = "pull_request",
    [string]$Delivery = "",
    [string]$BodyFile = ""
)

if ($Delivery -eq "") {
    $Delivery = [guid]::NewGuid().ToString()
}

if ($BodyFile -ne "") {
    $body = Get-Content -Raw $BodyFile
}
else {
    $repo = if ($env:GITHUB_TEST_REPO) { $env:GITHUB_TEST_REPO } else { "acme/core" }
    $prNumber = if ($env:GITHUB_TEST_PR) { $env:GITHUB_TEST_PR } else { "42" }
    $repoId = if ($env:GITHUB_TEST_REPO_ID) { $env:GITHUB_TEST_REPO_ID } else { "7" }
    $installId = if ($env:GITHUB_TEST_INSTALL_ID) { $env:GITHUB_TEST_INSTALL_ID } else { "1" }
    $headSha = if ($env:GITHUB_TEST_HEAD_SHA) { $env:GITHUB_TEST_HEAD_SHA } else { "abc123def456" }
    $sender = if ($env:GITHUB_TEST_SENDER) { $env:GITHUB_TEST_SENDER } else { "dev" }
    $parts = $repo.Split("/")
    $body = @"
{
  "action": "opened",
  "number": $prNumber,
  "pull_request": {
    "number": $prNumber,
    "draft": false,
    "merged": false,
    "head": {"sha": "$headSha", "ref": "feature"},
    "base": {"ref": "main"}
  },
  "repository": {"id": $repoId, "owner": {"login": "$($parts[0])"}, "name": "$($parts[1])"},
  "sender": {"login": "$sender"},
  "installation": {"id": $installId}
}
"@
}

$utf8 = New-Object System.Text.UTF8Encoding($false)
$bodyBytes = $utf8.GetBytes($body)
$hmac = New-Object System.Security.Cryptography.HMACSHA256
$hmac.Key = $utf8.GetBytes($Secret)
$sig = "sha256=" + [BitConverter]::ToString($hmac.ComputeHash($bodyBytes)).Replace("-", "").ToLower()

$headers = @{
    "Content-Type"       = "application/json"
    "X-GitHub-Event"     = $Event
    "X-GitHub-Delivery"  = $Delivery
    "X-Hub-Signature-256" = $sig
    "User-Agent"         = "GitHub-Hookshot/smoke"
}

try {
    $resp = Invoke-WebRequest -Uri $Url -Method Post -Headers $headers -Body $bodyBytes -UseBasicParsing
    Write-Host "delivery=$Delivery event=$Event status=$($resp.StatusCode)"
}
catch {
    $status = $_.Exception.Response.StatusCode.value__
    Write-Host "delivery=$Delivery event=$Event status=$status"
    if ($_.ErrorDetails.Message) {
        Write-Host $_.ErrorDetails.Message
    }
}
