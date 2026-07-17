param(
    [string]$ComposeFile = "docker-compose.yml",
    [string]$ClusterName = "gossip-local",
    [int]$TimeoutSeconds = 45
)

$ErrorActionPreference = "Stop"
$node1 = "$ClusterName-node1"
$network = "$ClusterName-net"
$ports = @(18080, 18081, 18082)
$restartRestored = $false
$connected = $true

function Get-Sum([int]$port) {
    return (Invoke-RestMethod -TimeoutSec 2 -Uri "http://localhost:$port/aggregate/sum").value
}

function Wait-ForSum([int[]]$targetPorts, [uint64]$expected) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $matches = $true
        foreach ($port in $targetPorts) {
            try {
                if ((Get-Sum $port) -ne $expected) {
                    $matches = $false
                }
            } catch {
                $matches = $false
            }
        }
        if ($matches) {
            return
        }
        Start-Sleep -Milliseconds 300
    } while ((Get-Date) -lt $deadline)
    throw "SUM did not converge to $expected on ports $($targetPorts -join ',')"
}

$baseline = Get-Sum 18080
foreach ($increment in @(2, 3, 5)) {
    Invoke-RestMethod -Method Post -Uri http://localhost:18080/update `
        -ContentType application/json `
        -Body "{`"aggregate_type`":`"SUM`",`"value`":$increment}" | Out-Null
}
$durableTarget = $baseline + 10
Wait-ForSum $ports $durableTarget

try {
    docker update --restart=no $node1 | Out-Null
    docker kill $node1 | Out-Null
    docker network disconnect -f $network $node1
    $connected = $false

    Invoke-RestMethod -Method Post -Uri http://localhost:18081/update `
        -ContentType application/json `
        -Body '{"aggregate_type":"SUM","value":7}' | Out-Null
    $clusterTarget = $durableTarget + 7
    Wait-ForSum @(18081, 18082) $clusterTarget

    docker start $node1 | Out-Null
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        try {
            $recovered = (docker exec $node1 wget -qO- http://127.0.0.1:8080/aggregate/sum | ConvertFrom-Json).value
        } catch {
            $recovered = $null
        }
        if ($recovered -eq $durableTarget) {
            break
        }
        Start-Sleep -Milliseconds 300
    } while ((Get-Date) -lt $deadline)
    if ($recovered -ne $durableTarget) {
        throw "isolated recovery returned SUM=$recovered, expected $durableTarget"
    }

    docker network connect --alias node1 $network $node1
    $connected = $true
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $rejoined = (docker exec $node1 wget -qO- http://127.0.0.1:8080/aggregate/sum | ConvertFrom-Json).value
        if ($rejoined -eq $clusterTarget) {
            break
        }
        Start-Sleep -Milliseconds 500
    } while ((Get-Date) -lt $deadline)
    if ($rejoined -ne $clusterTarget) {
        throw "rejoined node returned SUM=$rejoined, expected $clusterTarget"
    }

    docker update --restart=unless-stopped $node1 | Out-Null
    $restartRestored = $true
    docker compose -f $ComposeFile up -d --force-recreate node1 | Out-Null
    Wait-ForSum $ports $clusterTarget
    Write-Host "Crash recovery passed: recovered=$durableTarget converged=$clusterTarget" -ForegroundColor Green
} finally {
    if (-not $connected) {
        docker network connect --alias node1 $network $node1 | Out-Null
    }
    if (-not $restartRestored) {
        docker update --restart=unless-stopped $node1 | Out-Null
    }
}
