# Course Service API Testing Script (PowerShell)
# ทดสอบ APIs ทั้งหมดของ course service

$BaseUrl = "http://localhost:8000"

function Write-ColorOutput($ForegroundColor) {
    $fc = $host.UI.RawUI.ForegroundColor
    $host.UI.RawUI.ForegroundColor = $ForegroundColor
    if ($args) {
        Write-Output $args
    }
    $host.UI.RawUI.ForegroundColor = $fc
}

Write-Output "=========================================="
Write-Output "Course Service API Testing"
Write-Output "=========================================="
Write-Output ""

# Test 1: Circuit Breaker Status
Write-ColorOutput Yellow "[TEST 1] Testing Circuit Breaker Status..."
try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/metrics/circuit-breaker" -Method Get
    Write-ColorOutput Green "✓ PASS - Circuit Breaker Status"
    $response | ConvertTo-Json
} catch {
    Write-ColorOutput Red "✗ FAIL - $($_.Exception.Message)"
}
Write-Output ""

# Test 2: GET All Courses
Write-ColorOutput Yellow "[TEST 2] Getting all courses..."
try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/courses" -Method Get
    Write-ColorOutput Green "✓ PASS - GET /courses"
    $response | ConvertTo-Json
} catch {
    Write-ColorOutput Red "✗ FAIL - $($_.Exception.Message)"
}
Write-Output ""

# Test 3: POST Create New Course
Write-ColorOutput Yellow "[TEST 3] Creating a new course..."
$body = @{
    course_id = 999
    subject = "Docker Testing"
    credit = 3
    section = @("1")
    day_of_week = "Friday"
    start_time = "09:00:00"
    end_time = "12:00:00"
    capacity = 30
    state = "open"
    current_student = @()
    prerequisite = @()
} | ConvertTo-Json

try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/courses" -Method Post -Body $body -ContentType "application/json"
    Write-ColorOutput Green "✓ PASS - POST /courses"
    $response | ConvertTo-Json
} catch {
    Write-ColorOutput Red "✗ FAIL - $($_.Exception.Message)"
}
Write-Output ""

# Test 4: GET Single Course
Write-ColorOutput Yellow "[TEST 4] Getting course by ID (999)..."
try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/courses/999" -Method Get
    Write-ColorOutput Green "✓ PASS - GET /courses/999"
    $response | ConvertTo-Json
} catch {
    Write-ColorOutput Red "✗ FAIL - $($_.Exception.Message)"
}
Write-Output ""

# Test 5: PUT Update Course
Write-ColorOutput Yellow "[TEST 5] Updating course state to 'closed'..."
$updateBody = @{
    state = "closed"
    capacity = 50
} | ConvertTo-Json

try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/courses/999" -Method Put -Body $updateBody -ContentType "application/json"
    Write-ColorOutput Green "✓ PASS - PUT /courses/999"
    $response | ConvertTo-Json
} catch {
    Write-ColorOutput Red "✗ FAIL - $($_.Exception.Message)"
}
Write-Output ""

# Test 6: Verify Update
Write-ColorOutput Yellow "[TEST 6] Verifying update..."
try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/courses/999" -Method Get
    if ($response.state -eq "closed" -and $response.capacity -eq 50) {
        Write-ColorOutput Green "✓ PASS - Course updated correctly"
        Write-Output "State: $($response.state), Capacity: $($response.capacity)"
    } else {
        Write-ColorOutput Red "✗ FAIL - Update not reflected"
    }
} catch {
    Write-ColorOutput Red "✗ FAIL - $($_.Exception.Message)"
}
Write-Output ""

# Test 7: GET Metrics - Recent
Write-ColorOutput Yellow "[TEST 7] Getting recent metrics..."
try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/metrics/recent" -Method Get
    Write-ColorOutput Green "✓ PASS - GET /metrics/recent"
    Write-Output "Found $($response.count) metrics records"
    $response.metrics[0..2] | ConvertTo-Json
} catch {
    Write-ColorOutput Red "✗ FAIL - $($_.Exception.Message)"
}
Write-Output ""

# Test 8: GET Metrics - Aggregate
Write-ColorOutput Yellow "[TEST 8] Getting aggregate metrics..."
try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/metrics/aggregate" -Method Get
    Write-ColorOutput Green "✓ PASS - GET /metrics/aggregate"
    $response | ConvertTo-Json
} catch {
    Write-ColorOutput Red "✗ FAIL - $($_.Exception.Message)"
}
Write-Output ""

# Test 9: GET Metrics - Endpoint Stats
Write-ColorOutput Yellow "[TEST 9] Getting endpoint statistics..."
try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/metrics/endpoint/courses" -Method Get
    Write-ColorOutput Green "✓ PASS - GET /metrics/endpoint/courses"
    $response | ConvertTo-Json
} catch {
    Write-ColorOutput Red "✗ FAIL - $($_.Exception.Message)"
}
Write-Output ""

# Test 10: DELETE Course
Write-ColorOutput Yellow "[TEST 10] Deleting course 999..."
try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/courses/999" -Method Delete
    Write-ColorOutput Green "✓ PASS - DELETE /courses/999"
    $response | ConvertTo-Json
} catch {
    Write-ColorOutput Red "✗ FAIL - $($_.Exception.Message)"
}
Write-Output ""

# Test 11: Verify Deletion
Write-ColorOutput Yellow "[TEST 11] Verifying deletion (should return 404)..."
try {
    $response = Invoke-RestMethod -Uri "$BaseUrl/courses/999" -Method Get
    Write-ColorOutput Red "✗ FAIL - Expected 404, but got response"
} catch {
    if ($_.Exception.Response.StatusCode.value__ -eq 404) {
        Write-ColorOutput Green "✓ PASS - Course deleted successfully"
    } else {
        Write-ColorOutput Red "✗ FAIL - Expected 404, got $($_.Exception.Response.StatusCode.value__)"
    }
}
Write-Output ""

Write-Output "=========================================="
Write-Output "Testing Complete!"
Write-Output "=========================================="
