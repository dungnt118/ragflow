# =======================================================
# RagFlowMaster - Build & Deploy DockerHub (PowerShell)
# Tu dong dong goi, day Image len DockerHub & Trien khai
# Repo: dungnt118/freepbx:{RAGFLOW_TAG}
# =======================================================

[Console]::InputEncoding = [System.Text.Encoding]::UTF8
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
$Host.UI.RawUI.WindowTitle = "RagFlow - Build and Deploy Pipeline"

Write-Host "=======================================================" -ForegroundColor Cyan
Write-Host "   RagFlowMaster - Build, Push DockerHub & Deploy" -ForegroundColor Cyan
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

Write-Host "[THONG TIN CAU HINH DEPLOYMENT]" -ForegroundColor Yellow
Write-Host " - Cau hinh tu file:    $envFile"
Write-Host " - Registry Repository: $repo"
Write-Host " - DockerHub Username:  $username"
Write-Host " - Service Image Tag:   $ragflowImage"
Write-Host " - Target Engine:       Go Server + React Frontend + Nginx (Production Stage)"
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

# 6. Trien khai Local & Tai khoi dong Service
$deployPrompt = 'Ban co muon restart/redeploy dich vu RagFlow local qua Portainer Stack khong? [Y/N] (Mac dinh: Y): '
$shouldDeploy = Read-Host -Prompt $deployPrompt
if (-not $shouldDeploy -or $shouldDeploy.Trim().ToUpper() -eq "Y") {
    Write-Host "[4/4] Khoi dong lai dich vu qua Portainer Stack..." -ForegroundColor Cyan
    $stackFile = Join-Path $rootDir "portainer-stack.yaml"
    if (Test-Path $stackFile) {
        docker compose -f $stackFile --env-file $envFile up -d ragflow
    } else {
        docker compose up -d ragflow
    }
    
    if ($LASTEXITCODE -eq 0) {
        Write-Host "[OK] RagFlow Server Container da duoc khoi chay thanh cong!" -ForegroundColor Green
        Write-Host "Kiem tra Web UI tai: http://localhost:$webPort" -ForegroundColor Cyan
    } else {
        Write-Host "[LOI] Khong the khoi chay container!" -ForegroundColor Red
    }
} else {
    Write-Host "[4/4] Bo qua buoc deploy local theo lua chon cua nguoi dung." -ForegroundColor Yellow
}

Write-Host ""
Write-Host "=======================================================" -ForegroundColor Green
Write-Host "   QUY TRINH BUILD & DEPLOY RAGFLOW HOAN TAT!" -ForegroundColor Green
Write-Host "=======================================================" -ForegroundColor Green
Read-Host "Nhan Enter de dong cua so..."
