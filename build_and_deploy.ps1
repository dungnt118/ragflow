# =======================================================
# RagFlowMaster - Build, Push DockerHub & Local Deploy
# Tự động đóng gói, đẩy Image lên DockerHub & cập nhật Local
# Repo: dungnt118/freepbx:{RAGFLOW_TAG}
# =======================================================

[Console]::InputEncoding = [System.Text.Encoding]::UTF8
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
$Host.UI.RawUI.WindowTitle = "RagFlow - Build, Push & Local Deploy Pipeline"

Write-Host "=======================================================" -ForegroundColor Cyan
Write-Host "   RagFlowMaster - Build, Push DockerHub & Local Run" -ForegroundColor Cyan
Write-Host "=======================================================" -ForegroundColor Cyan
Write-Host ""

$rootDir = $PSScriptRoot
if (-not $rootDir) {
    $rootDir = Get-Location
}

# 1. Tim va doc file cau hinh (Uu tien portainer.env, sau do toi .env)
$envFile = Join-Path $rootDir "portainer.env"
if (-not (Test-Path $envFile)) {
    $envFile = Join-Path $rootDir ".env"
}

if (-not (Test-Path $envFile)) {
    Write-Host "[CANH BAO] Khong tim thay file portainer.env hoac .env!" -ForegroundColor Yellow
    Write-Host "He thong se su dung gia tri cau hinh mac dinh." -ForegroundColor Yellow
}

$envConfig = @{}
if (Test-Path $envFile) {
    Get-Content $envFile -Encoding UTF8 | ForEach-Object {
        $line = $_.Trim()
        if ($line -and -not $line.StartsWith("#")) {
            $idx = $line.IndexOf("=")
            if ($idx -gt 0) {
                $key = $line.Substring(0, $idx).Trim()
                $val = $line.Substring($idx + 1).Trim().Trim('"').Trim("'")
                $envConfig[$key] = $val
            }
        }
    }
}

$repo = if ($envConfig["DOCKER_IMAGE_REPO"]) { $envConfig["DOCKER_IMAGE_REPO"] } else { "dungnt118/freepbx" }
$ragflowTag = if ($envConfig["RAGFLOW_TAG"]) { $envConfig["RAGFLOW_TAG"] } else { "ragflow" }
$username = if ($envConfig["DOCKERHUB_USERNAME"]) { $envConfig["DOCKERHUB_USERNAME"] } else { "dungnt118" }
$token = $envConfig["DOCKERHUB_TOKEN"]
$webPort = if ($envConfig["RAGFLOW_WEB_PORT"]) { $envConfig["RAGFLOW_WEB_PORT"] } else { "3060" }

$ragflowImage = if ($envConfig["RAGFLOW_IMAGE"]) { $envConfig["RAGFLOW_IMAGE"] } else { "$($repo):$($ragflowTag)" }

$portainerUrl = if ($envConfig["PORTAINER_URL"]) { $envConfig["PORTAINER_URL"].TrimEnd("/") } else { "" }
$portainerToken = $envConfig["PORTAINER_ACCESS_TOKEN"]
$portainerEndpointId = if ($envConfig["PORTAINER_ENDPOINT_ID"]) { [int]$envConfig["PORTAINER_ENDPOINT_ID"] } else { 3 }
$portainerStackId = if ($envConfig["PORTAINER_STACK_ID"]) { [int]$envConfig["PORTAINER_STACK_ID"] } else { 110 }
$portainerStackName = if ($envConfig["PORTAINER_STACK_NAME"]) { $envConfig["PORTAINER_STACK_NAME"] } else { "demego-faq-rag" }

Write-Host "[THONG TIN CAU HINH]" -ForegroundColor Yellow
Write-Host " - File cau hinh:       $envFile"
Write-Host " - DockerHub Repo:      $repo"
Write-Host " - DockerHub Username:  $username"
Write-Host " - Image Tag:           $ragflowImage"
Write-Host " - Local Web Port:      http://localhost:$webPort"
Write-Host " - Moi truong local:    Docker Desktop (Docker Compose)"
if ($portainerUrl -and $portainerToken) {
    Write-Host " - Portainer Prod:      $portainerUrl (Stack: $portainerStackName #$portainerStackId)"
}
Write-Host ""

# 2. Kiem tra Docker Daemon
docker info *> $null
if ($LASTEXITCODE -ne 0) {
    Write-Host "[LOI] Docker daemon chua khoi dong hoac chua chay!" -ForegroundColor Red
    Write-Host "Vui long mo ung dung Docker Desktop truoc khi chay script nay." -ForegroundColor Yellow
    Read-Host "Nhan Enter de thoat..."
    exit 1
}

# 3. Dang nhap DockerHub
if ($token -and $username) {
    Write-Host "[1/4] Dang dang nhap DockerHub voi tai khoan $username..." -ForegroundColor Cyan
    $token | docker login -u $username --password-stdin
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[OK] Dang nhap DockerHub thanh cong!" -ForegroundColor Green
    } else {
        Write-Host "[CANH BAO] Dang nhap bang token khong thanh cong. Su dung phien dang nhap hien tai tren Docker Desktop." -ForegroundColor Yellow
    }
} else {
    Write-Host "[1/4] Khong co token rieng, su dung phien dang nhap hien tai tren Docker Desktop." -ForegroundColor Yellow
}
Write-Host ""

# 4. Build Docker Image
Write-Host "[2/4] Bat dau Build Docker Image: $ragflowImage ..." -ForegroundColor Cyan
$dockerFile = Join-Path $rootDir "Dockerfile"

if (-not (Test-Path $dockerFile)) {
    Write-Host "[LOI] Khong tim thay Dockerfile tai thu muc goc: $rootDir" -ForegroundColor Red
    Read-Host "Nhan Enter de thoat..."
    exit 1
}

# Bat BuildKit de toi uu hoa cache mount
$env:DOCKER_BUILDKIT = 1

docker build -t $ragflowImage -f $dockerFile $rootDir
if ($LASTEXITCODE -ne 0) {
    Write-Host "[LOI] Build RagFlow Docker image that bai!" -ForegroundColor Red
    Read-Host "Nhan Enter de thoat..."
    exit 1
}
Write-Host "[OK] Build RagFlow image thanh cong!" -ForegroundColor Green
Write-Host ""

# 5. Push Docker Image
$pushPrompt = 'Ban co muon push image len Docker Hub ngay bay gio khong? [Y/N] (Mac dinh: Y): '
$shouldPush = Read-Host -Prompt $pushPrompt
if (-not $shouldPush -or $shouldPush.Trim().ToUpper() -eq "Y") {
    Write-Host "[3/4] Dang day - Push Image len Docker Hub: $ragflowImage ..." -ForegroundColor Cyan
    docker push $ragflowImage
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[LOI] Push RagFlow image that bai!" -ForegroundColor Red
        Read-Host "Nhan Enter de thoat..."
        exit 1
    }
    Write-Host "[OK] Push RagFlow image len Docker Hub thanh cong!" -ForegroundColor Green
} else {
    Write-Host "[3/4] Bo qua buoc Push Docker Hub theo lua chon cua nguoi dung." -ForegroundColor Yellow
}
Write-Host ""

# Ham ho tro Deploy len Portainer Production qua REST API
function Deploy-ToPortainerProduction {
    param (
        [string]$url,
        [string]$apiKey,
        [int]$endpointId,
        [int]$stackId,
        [string]$stackName,
        [string]$composeFilePath,
        [hashtable]$envMap
    )

    Write-Host "[5/5] Dang ket noi va chuan bi deploy len Portainer Production..." -ForegroundColor Cyan
    Write-Host " - Server:      $url"
    Write-Host " - Stack:       $stackName (ID: $stackId | Endpoint: $endpointId)"

    if (-not (Test-Path $composeFilePath)) {
        Write-Host "[LOI] Khong tim thay file stack: $composeFilePath" -ForegroundColor Red
        return $false
    }
    $composeContent = Get-Content $composeFilePath -Raw -Encoding UTF8

    # Chuyen danh sach envMap thanh mang object cho Portainer API
    $envArray = @()
    foreach ($k in $envMap.Keys) {
        $envArray += @{
            name  = $k
            value = [string]$envMap[$k]
        }
    }

    $bodyObject = @{
        stackFileContent = $composeContent
        env              = $envArray
        prune            = $true
        pullImage        = $true
    }

    $jsonBody = $bodyObject | ConvertTo-Json -Depth 10
    $requestUri = "$url/api/stacks/$($stackId)?endpointId=$($endpointId)"
    $headers = @{
        "X-API-Key"    = $apiKey
        "Content-Type" = "application/json; charset=utf-8"
    }

    try {
        Write-Host " -> Dang gui lenh cap nhat Stack len Portainer..." -ForegroundColor Cyan
        Write-Host " -> [Auto Pull Image] = TRUE (Yeu cau Portainer keo image moi tu Docker Hub)" -ForegroundColor Cyan
        Write-Host " -> [Prune Services]  = TRUE" -ForegroundColor Cyan

        $utf8Bytes = [System.Text.Encoding]::UTF8.GetBytes($jsonBody)
        $response = Invoke-RestMethod -Uri $requestUri -Method Put -Headers $headers -Body $utf8Bytes -TimeoutSec 300

        Write-Host "[OK] Portainer Production da tiep nhan va redeploy Stack '$stackName' thanh cong!" -ForegroundColor Green
        if ($response.Id) {
            Write-Host " -> Stack ID: $($response.Id) | Trang thai Status: $($response.Status)" -ForegroundColor Green
        }
        return $true
    } catch {
        Write-Host "[LOI] Redeploy Portainer that bai!" -ForegroundColor Red
        Write-Host "Chi tiet loi: $($_.Exception.Message)" -ForegroundColor Red
        if ($_.Exception.Response) {
            try {
                $stream = $_.Exception.Response.GetResponseStream()
                $reader = New-Object System.IO.StreamReader($stream)
                Write-Host "Phan hoi tu Portainer: $($reader.ReadToEnd())" -ForegroundColor Red
            } catch {}
        }
        return $false
    }
}

# 6. Trien khai Local tren Docker Desktop
$deployPrompt = 'Ban co muon restart/cap nhat container RagFlow tren Docker Desktop (Local) khong? [Y/N] (Mac dinh: Y): '
$shouldDeployLocal = Read-Host -Prompt $deployPrompt
if (-not $shouldDeployLocal -or $shouldDeployLocal.Trim().ToUpper() -eq "Y") {
    Write-Host "[4/5] Dang cap nhat va khoi dong lai container RagFlow tren Docker Desktop..." -ForegroundColor Cyan
    $composeFile = Join-Path $rootDir "portainer-stack.yaml"
    if (Test-Path $composeFile) {
        docker compose -f $composeFile --env-file $envFile up -d ragflow
    } else {
        docker compose up -d ragflow
    }
    
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[OK] Container RagFlow da duoc khoi chay thanh cong tren Docker Desktop!" -ForegroundColor Green
        Write-Host "Kiem tra Web UI Local: http://localhost:$webPort" -ForegroundColor Cyan
    } else {
        Write-Host "[LOI] Khong the khoi chay container tren Docker Desktop!" -ForegroundColor Red
    }
} else {
    Write-Host "[4/5] Bo qua buoc khoi chay container tren Docker Desktop theo lua chon cua ban." -ForegroundColor Yellow
}
Write-Host ""

# 7. Trien khai Production qua Portainer API (neu co cau hinh)
if ($portainerUrl -and $portainerToken -and $portainerStackId) {
    $prodPrompt = "Ban co muon tu dong redeploy Stack '$portainerStackName' (#$portainerStackId) len Portainer Production khong? [Y/N] (Mac dinh: Y): "
    $shouldDeployProd = Read-Host -Prompt $prodPrompt
    if (-not $shouldDeployProd -or $shouldDeployProd.Trim().ToUpper() -eq "Y") {
        $stackFile = Join-Path $rootDir "portainer-stack.yaml"
        Deploy-ToPortainerProduction -url $portainerUrl -apiKey $portainerToken -endpointId $portainerEndpointId -stackId $portainerStackId -stackName $portainerStackName -composeFilePath $stackFile -envMap $envConfig
    } else {
        Write-Host "[5/5] Bo qua buoc redeploy Portainer Production theo lua chon cua ban." -ForegroundColor Yellow
    }
    Write-Host ""
}

Write-Host "=======================================================" -ForegroundColor Green
Write-Host "   QUY TRINH BUILD & DEPLOY PIPELINE HOAN TAT!" -ForegroundColor Green
Write-Host "=======================================================" -ForegroundColor Green
if ($portainerUrl -and $portainerStackId) {
    Write-Host "Link kiem tra Portainer: $portainerUrl/#!/$portainerEndpointId/docker/stacks/$portainerStackName?id=$portainerStackId&type=2" -ForegroundColor Cyan
}
Read-Host "Nhan Enter de dong cua so..."
