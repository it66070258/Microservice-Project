package main

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sony/gobreaker"
)

// MetricsMiddleware เป็น middleware สำหรับเก็บข้อมูล metrics
func MetricsMiddleware(metricsLogger *MetricsLogger, cb *gobreaker.CircuitBreaker) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()

		// ให้ request ทำงานต่อ
		c.Next()

		// คำนวณ response time
		responseTime := time.Since(startTime).Milliseconds()

		// เก็บข้อมูล circuit breaker state
		cbState := cb.State().String()

		// เก็บ error message ถ้ามี
		errorMsg := ""
		if len(c.Errors) > 0 {
			errorMsg = c.Errors.String()
		}

		// สร้าง metric
		metric := RequestMetric{
			Timestamp:           startTime,
			Endpoint:            c.Request.URL.Path,
			Method:              c.Request.Method,
			StatusCode:          c.Writer.Status(),
			ResponseTimeMs:      float64(responseTime),
			CircuitBreakerState: cbState,
			ErrorMessage:        errorMsg,
		}

		// บันทึกลง database (async เพื่อไม่ให้กระทบ performance)
		go func() {
			if err := metricsLogger.LogRequest(metric); err != nil {
				// Log error แต่ไม่ให้กระทบการทำงานหลัก
				// สามารถเพิ่ม logger ที่นี่ได้
			}
		}()
	}
}
