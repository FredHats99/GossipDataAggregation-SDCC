param(
    [string]$Image = "gossip-agg:local",
    [int]$PortBase = 0,
    [int]$TimeoutSeconds = 45
)

$ErrorActionPreference = "Stop"
$suffix = $PID
$network = "gossip-membership-e2e-$suffix"
$containers = @(
    "gossip-membership-node1-$suffix",
    "gossip-membership-node2-$suffix",
    "gossip-membership-node3-$suffix"
)

function Get-FreeTcpPort {
    $listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    $listener.Start()
    try {
        return ([System.Net.IPEndPoint]$listener.LocalEndpoint).Port
    } finally {
        $listener.Stop()
    }
}

if ($PortBase -gt 0) {
    $ports = @($PortBase, ($PortBase + 1), ($PortBase + 2))
} else {
    $ports = @()
    while ($ports.Count -lt 3) {
        $candidate = Get-FreeTcpPort
        if ($candidate -notin $ports) {
            $ports += $candidate
        }
    }
}

function Invoke-Docker([string[]]$DockerArgs) {
    & docker @DockerArgs
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($DockerArgs -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Start-Node([int]$Index, [string]$NodeID, [string]$Seeds) {
    $arguments = @(
        "run", "-d", "--rm",
        "--name", $containers[$Index],
        "--network", $network,
        "--network-alias", $NodeID,
        "-p", "$($ports[$Index]):8080",
        "-e", "NODE_ID=$NodeID",
        "-e", "HTTP_ADDR=:8080",
        "-e", "BIND_ADDR=0.0.0.0:7000",
        "-e", "SEED_NODES=$Seeds",
        "-e", "GOSSIP_INTERVAL_MS=100",
        "-e", "ANTI_ENTROPY_INTERVAL_MS=500",
        "-e", "FANOUT=2",
        "-e", "DATA_DIR=/tmp/gossip",
        "-e", "SNAPSHOT_INTERVAL_SECONDS=60",
        "-e", "LOG_LEVEL=info",
        $Image
    )
    Invoke-Docker $arguments | Out-Null
}

function Wait-ForReady {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $ready = 0
        foreach ($port in $ports) {
            try {
                if ((Invoke-WebRequest -UseBasicParsing -TimeoutSec 2 -Uri "http://localhost:$port/readyz").StatusCode -eq 200) {
                    $ready++
                }
            } catch {
                # Startup is still in progress.
            }
        }
        if ($ready -eq 3) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ((Get-Date) -lt $deadline)
    throw "not all nodes became ready within $TimeoutSeconds seconds"
}

function Wait-ForMembership {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $converged = 0
        foreach ($port in $ports) {
            try {
                $members = (Invoke-RestMethod -TimeoutSec 2 -Uri "http://localhost:$port/members").members
                $alive = @($members | Where-Object { $_.status -eq "alive" })
                $ids = @($alive.node_id | Sort-Object)
                $incarnationsValid = @($alive | Where-Object { $_.incarnation -gt 0 }).Count -eq 3
                if (($ids -join ",") -eq "node1,node2,node3" -and $incarnationsValid) {
                    $converged++
                }
            } catch {
                # Membership is still converging.
            }
        }
        if ($converged -eq 3) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ((Get-Date) -lt $deadline)
    throw "partial-seed membership did not converge within $TimeoutSeconds seconds"
}

function Wait-ForSum([uint64]$Expected) {
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    do {
        $matches = 0
        foreach ($port in $ports) {
            try {
                if ((Invoke-RestMethod -TimeoutSec 2 -Uri "http://localhost:$port/aggregate/sum").value -eq $Expected) {
                    $matches++
                }
            } catch {
                # Aggregate gossip is still converging.
            }
        }
        if ($matches -eq 3) {
            return
        }
        Start-Sleep -Milliseconds 250
    } while ((Get-Date) -lt $deadline)
    throw "SUM did not converge to $Expected within $TimeoutSeconds seconds"
}

try {
    Invoke-Docker @("network", "create", $network) | Out-Null

    # Chain topology: node1 has no seed, node2 knows node1, node3 knows node2.
    Start-Node 0 "node1" ""
    Start-Node 1 "node2" "node1:7000"
    Start-Sleep -Milliseconds 500
    Start-Node 2 "node3" "node2:7000"

    Wait-ForReady
    Wait-ForMembership

    Invoke-RestMethod -Method Post -Uri "http://localhost:$($ports[2])/update" `
        -ContentType "application/json" `
        -Body '{"aggregate_type":"SUM","value":11}' | Out-Null
    Wait-ForSum 11

    Write-Host "Membership dissemination passed: partial seeds converged and SUM=11 reached all nodes" -ForegroundColor Green
} finally {
    foreach ($container in $containers) {
        $existingContainer = & docker ps -aq --filter "name=^/$container$"
        if ($existingContainer) {
            & docker rm -f $container | Out-Null
        }
    }
    $existingNetwork = & docker network ls -q --filter "name=^$network$"
    if ($existingNetwork) {
        & docker network rm $network | Out-Null
    }
}
