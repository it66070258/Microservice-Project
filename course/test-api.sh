#!/bin/bash

# Course Service API Testing Script
# ทดสอบ APIs ทั้งหมดของ course service

BASE_URL="http://localhost:8000"
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=========================================="
echo "Course Service API Testing"
echo "=========================================="
echo ""

# Test 1: Health Check (GET /metrics/circuit-breaker)
echo -e "${YELLOW}[TEST 1]${NC} Testing Circuit Breaker Status..."
response=$(curl -s -w "\n%{http_code}" $BASE_URL/metrics/circuit-breaker)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ PASS${NC} - Circuit Breaker Status"
    echo "$body" | jq '.'
else
    echo -e "${RED}✗ FAIL${NC} - HTTP $http_code"
    echo "$body"
fi
echo ""

# Test 2: GET All Courses
echo -e "${YELLOW}[TEST 2]${NC} Getting all courses..."
response=$(curl -s -w "\n%{http_code}" $BASE_URL/courses)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ PASS${NC} - GET /courses"
    echo "$body" | jq '.'
else
    echo -e "${RED}✗ FAIL${NC} - HTTP $http_code"
    echo "$body"
fi
echo ""

# Test 3: POST Create New Course
echo -e "${YELLOW}[TEST 3]${NC} Creating a new course..."
response=$(curl -s -w "\n%{http_code}" -X POST $BASE_URL/courses \
  -H "Content-Type: application/json" \
  -d '{
    "course_id": 999,
    "subject": "Docker Testing",
    "credit": 3,
    "section": ["1"],
    "day_of_week": "Friday",
    "start_time": "09:00:00",
    "end_time": "12:00:00",
    "capacity": 30,
    "state": "open",
    "current_student": [],
    "prerequisite": []
  }')
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "201" ]; then
    echo -e "${GREEN}✓ PASS${NC} - POST /courses"
    echo "$body" | jq '.'
else
    echo -e "${RED}✗ FAIL${NC} - HTTP $http_code"
    echo "$body"
fi
echo ""

# Test 4: GET Single Course
echo -e "${YELLOW}[TEST 4]${NC} Getting course by ID (999)..."
response=$(curl -s -w "\n%{http_code}" $BASE_URL/courses/999)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ PASS${NC} - GET /courses/999"
    echo "$body" | jq '.'
else
    echo -e "${RED}✗ FAIL${NC} - HTTP $http_code"
    echo "$body"
fi
echo ""

# Test 5: PUT Update Course
echo -e "${YELLOW}[TEST 5]${NC} Updating course state to 'closed'..."
response=$(curl -s -w "\n%{http_code}" -X PUT $BASE_URL/courses/999 \
  -H "Content-Type: application/json" \
  -d '{
    "state": "closed",
    "capacity": 50
  }')
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ PASS${NC} - PUT /courses/999"
    echo "$body" | jq '.'
else
    echo -e "${RED}✗ FAIL${NC} - HTTP $http_code"
    echo "$body"
fi
echo ""

# Test 6: Verify Update
echo -e "${YELLOW}[TEST 6]${NC} Verifying update..."
response=$(curl -s -w "\n%{http_code}" $BASE_URL/courses/999)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    state=$(echo "$body" | jq -r '.state')
    capacity=$(echo "$body" | jq -r '.capacity')

    if [ "$state" = "closed" ] && [ "$capacity" = "50" ]; then
        echo -e "${GREEN}✓ PASS${NC} - Course updated correctly"
        echo "State: $state, Capacity: $capacity"
    else
        echo -e "${RED}✗ FAIL${NC} - Update not reflected"
        echo "$body" | jq '.'
    fi
else
    echo -e "${RED}✗ FAIL${NC} - HTTP $http_code"
fi
echo ""

# Test 7: GET Metrics - Recent
echo -e "${YELLOW}[TEST 7]${NC} Getting recent metrics..."
response=$(curl -s -w "\n%{http_code}" $BASE_URL/metrics/recent)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ PASS${NC} - GET /metrics/recent"
    count=$(echo "$body" | jq '.count')
    echo "Found $count metrics records"
    echo "$body" | jq '.metrics[0:3]'
else
    echo -e "${RED}✗ FAIL${NC} - HTTP $http_code"
    echo "$body"
fi
echo ""

# Test 8: GET Metrics - Aggregate
echo -e "${YELLOW}[TEST 8]${NC} Getting aggregate metrics..."
response=$(curl -s -w "\n%{http_code}" $BASE_URL/metrics/aggregate)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ PASS${NC} - GET /metrics/aggregate"
    echo "$body" | jq '.'
else
    echo -e "${RED}✗ FAIL${NC} - HTTP $http_code"
    echo "$body"
fi
echo ""

# Test 9: GET Metrics - Endpoint Stats
echo -e "${YELLOW}[TEST 9]${NC} Getting endpoint statistics..."
response=$(curl -s -w "\n%{http_code}" $BASE_URL/metrics/endpoint/courses)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ PASS${NC} - GET /metrics/endpoint/courses"
    echo "$body" | jq '.'
else
    echo -e "${RED}✗ FAIL${NC} - HTTP $http_code"
    echo "$body"
fi
echo ""

# Test 10: DELETE Course
echo -e "${YELLOW}[TEST 10]${NC} Deleting course 999..."
response=$(curl -s -w "\n%{http_code}" -X DELETE $BASE_URL/courses/999)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "200" ]; then
    echo -e "${GREEN}✓ PASS${NC} - DELETE /courses/999"
    echo "$body" | jq '.'
else
    echo -e "${RED}✗ FAIL${NC} - HTTP $http_code"
    echo "$body"
fi
echo ""

# Test 11: Verify Deletion
echo -e "${YELLOW}[TEST 11]${NC} Verifying deletion (should return 404)..."
response=$(curl -s -w "\n%{http_code}" $BASE_URL/courses/999)
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | sed '$d')

if [ "$http_code" = "404" ]; then
    echo -e "${GREEN}✓ PASS${NC} - Course deleted successfully"
else
    echo -e "${RED}✗ FAIL${NC} - Expected 404, got HTTP $http_code"
    echo "$body"
fi
echo ""

echo "=========================================="
echo "Testing Complete!"
echo "=========================================="
