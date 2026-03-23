# โปรเจคระบบลงทะเบียนเรียน (Registration System)

โปรเจคนี้ คือ ระบบลงทะเบียนเรียนที่พัฒนาด้วยสถาปัตยกรรม **Microservices** โดยใช้ภาษา **Go (Golang)** และเทคโนโลยีที่ทันสมัยเพื่อรองรับการขยายตัว (Scalability) และความทนทาน (Resilience) ของระบบ

---

## 🛠️ เทคนิคและเทคโนโลยีที่ใช้ในโปรเจค

1. **Microservices Architecture**: ระบบถูกแบ่งออกเป็น 3 เซอร์วิสหลักที่ทำงานแยกจากกันอย่างชัดเจน
   - **Course Service** (Port `8000`): จัดการข้อมูลรายวิชา การเปิดรับ และควบคุณโควต้าที่นั่ง
   - **Student Service** (Port `8001`): จัดการข้อมูลนักศึกษา ประวัติส่วนตัว และวิชาที่สอบผ่านแล้ว (Gradoded Subjects)
   - **Enrollment Service** (Port `8002`): ดูแลตรรกะการลงทะเบียนเรียน ตรวจสอบวิชาบังคับก่อน (Prerequisite) และตรวจสอบเวลาเรียนที่ชนกัน

2. **Go & Gin Framework**:
   - ตัวระบบถูกพัฒนาด้วยภาษา Go (เวอร์ชั่น `1.25.4`) ซึ่งโดดเด่นรวดเร็วและใช้ทรัพยากรน้อย
   - รัน Web Service และจัดการ Routing ของ RESTful API ด้วยโมดูล `gin-gonic/gin`

3. **PostgreSQL**:
   - เป็น Relational Database หลักสำหรับจัดการข้อมูล
   - มีการทำ Auto-Initialization (Data Seeding และ DB Schema) ผ่านการ Mount Docker Volume (`/docker-entrypoint-initdb.d/`) ทันทีที่ DB เริ่มทำงาน

4. **RabbitMQ (Message Broker)**:
   - ใช้เป็นตัวกลางช่วยให้สามารถสื่อสารแบบ Asynchronous (Event-driven) ระหว่างเซอร์วิส ช่วยลดคอขวดสะสมเมื่อมีผู้เรียกใช้งานลงทะเบียนเรียนเข้ามาเยอะๆ พร้อมกัน

5. **Resiliency & Fault Tolerance (Circuit Breaker)**:
   - ป้องกันปัญหาระบบล่มแบบโดมิโน่ ด้วยการใช้ไลบรารี `sony/gobreaker` ในการทำ **Circuit Breaker** (เช็คสถานะและตัดวงจรหากเซอร์วิสอื่นไม่ตอบสนอง)

6. **Service Discovery (Consul)**:
   - นำ **HashiCorp Consul** เพื่อใช้เป็น Service Discovery ช่วยให้ Microservices แต่อันตระหนักรู้และง่ายต่อการเชื่อมต่อและทำ Health Check ตรวจสอบกันเอง

7. **Monitoring & Observability (ตรวจวัดสถานะระบบ)**:
   - **Prometheus** (Port `9090`): คอยดึงข้อมูล (Scrape) metrics เช่น Request Count และ Latency ของแต่ละ Services เพื่อเก็บข้อมูลในรูปแบบ Time-series database
   - **Grafana** (Port `3000`): ระบบแสดงผล Visualize ข้อมูลให้อยู่ในรูปแบบ Dashboard ที่ดูเข้าใจง่ายและอัปเดตแบบเรียลไทม์

8. **Docker & Docker Compose**:
   - ควบคุมทุก Environment ให้อยู่ข้างในระบบ Containerization
   - มีการบิวต์ Docker ด้วยแนวคิด **Multi-stage Build** (Go Build Stage > Alpine Run Stage) ซึ่งขนาดได้ Image file สุดท้ายของ Service ที่เล็กและปลอดภัย
   - ผสานการสั่งการทุกคอนเทนเนอร์เข้าด้วยกันพร้อมระบบ Network ที่เชื่อมกันผ่านคำสั่ง `docker compose` เพียงอย่างเดียว

9. **Race Condition Handling**:
   - มีการจัดการปัญหา Race Condition เพื่อป้องกันความผิดพลาดเมื่อมีคำร้องขอเข้ามาทำงานกับข้อมูลเดียวกันพร้อมๆ กัน (เช่น การลงทะเบียนและหักโควต้าที่นั่ง)

10. **RPC (Remote Procedure Call) Pattern**:
    - มีการออกแบบการสื่อสารระหว่าง Microservices ด้วยรูปแบบ RPC เพื่อให้แต่ละเซอร์วิสสามารถเรียกใช้งานบริการของเซอร์วิสอื่นได้อย่างรวดเร็วและเป็นระบบ

---

## 🚀 คู่มือการใช้งาน

### 1. การเริ่มต้นใช้งาน

สิ่งที่จำเป็นต้องติดตั้งก่อนใช้งาน:

- **Go (Golang)**
- **Docker**

### 2. การเปิดและรันระบบ

โคลนโปรเจค และใช้งาน Docker Compose เพื่อเริ่มใช้งาน:

```bash
https://github.com/it66070258/Microservice-Project.git
```
```bash
docker compose up -d --build
```

### 3. การทดสอบใช้งานส่งคำสั่ง API

หลังจากระบบเริ่มต้นสำเร็จ (รวมถึงจัดการ Seed Database ของ Postgres เรียบร้อยแล้ว) สามารถทดสอบยิง API คร่าวๆ ได้ดังนี้ (ด้วยโปรแกรมอย่าง Postman, cURL หรือ Thunder Client):

**🌐 Course Service (จัดการรายวิชา)**

- ดึงรายการวิชาทั้งหมด: `GET http://localhost:8000/courses`
- ดึงข้อมูลวิชารหัส 9: `GET http://localhost:8000/courses/9`
- แก้ไขข้อมูลวิชา: `PUT http://localhost:8000/courses/9`
  ```json
  {
    "subject": "Advanced Mathematics",
    "capacity": 50,
    "state": "closed"
  }
  ```
- เพิ่มรายวิชาใหม่: `POST http://localhost:8000/courses`
  ```json
  {
    "course_id": 19,
    "subject": "Chemistry",
    "credit": 3,
    "section": ["1", "2"],
    "day_of_week": "Thursday",
    "start_time": "09:00:00",
    "end_time": "12:00:00",
    "capacity": 40,
    "state": "open",
    "prerequisite": null
  }
  ```
- ลบรายวิชา: `DELETE http://localhost:8000/courses/9`

**🌐 Student Service (จัดการนักศึกษา)**

- ดูข้อมูลนักศึกษาทั้งหมด: `GET http://localhost:8001/students`
- ดูข้อมูลนักศึกษารหัส 2: `GET http://localhost:8001/students/2`
- สมัครสมาชิก: `POST http://localhost:8001/register`
  ```json
  {
    "student_id": 101,
    "first_name": "Somsak",
    "last_name": "Meedee",
    "email": "somsak.m@example.com",
    "password": "password123",
    "birthdate": "2005-01-01",
    "gender": "Male",
    "year_level": 1,
    "graded_subject": ["Mathematics", "Physics"]
  }
  ```
- เข้าสู่ระบบ: `POST http://localhost:8001/login`
  ```json
  {
    "email": "somsak.m@example.com",
    "password": "password123"
  }
  ```
- ดูโปรไฟล์ปัจจุบัน: `GET http://localhost:8001/profile`
- แก้ไขโปรไฟล์: `PUT http://localhost:8001/profile`
  ```json
  {
    "first_name": "Somsak-Update",
    "last_name": "Meedee-New",
    "birthdate": "2005-01-01",
    "gender": "Male",
    "year_level": 2
  }
  ```
- ออกจากระบบ: `POST http://localhost:8001/logout`

**🌐 Enrollment Service (ระบบลงทะเบียน)**

- ซื้อขาย/ลงทะเบียนรายวิชา: `POST http://localhost:8002/enroll`
  ```json
  {
    "student_id": 1,
    "course_ids": [15]
  }
  ```

### 4. การทดสอบ Monitoring (Prometheus & Grafana)

**4.1 สร้างข้อมูลเพื่อจำลอง Request (Refresh หลายๆ ครั้งเพื่อสร้าง Traffic)**
ให้เรียกไปที่ API ต่างๆ ของระบบที่ได้เพิ่มระบบจับ Metric ไว้แล้ว (สามารถเปิดใน Browser หรือใช้ Postman) Refresh แต่ละหน้าสัก 5-10 รอบ โดยเฉพาะตัวอย่างเหล่านี้:

- ระบบแสดงรายวิชา: `http://localhost:8000/courses`
- ระบบข้อมูลคลาสเรียน: `http://localhost:8000/courses/1`
- ระบบนักเรียน: `http://localhost:8001/students`

**4.2 ตั้งค่าการดึงข้อมูลใน Grafana**

- เปิด Browser เข้าไปที่ `http://localhost:3000` (Grafana)
- ใส่ User: `admin` และ Pass: `admin`
- ในหน้าแรก ไปที่ **Connections** > แล้วเลือก **Data Sources**
- คลิก **Add data source**
- เลือก **Prometheus**
- ในช่อง Prometheus server URL: ให้ใส่ `http://prometheus:9090/` _(สำคัญ: เนื่องจากมันอยู่ใน Docker Network เดียวกัน ต้องเรียกชื่อ service ว่า prometheus ไม่ใช่ localhost)_
- เลื่อนลงมาด้านล่างสุดและกดปุ่ม **Save & test** (ถ้าสำเร็จจะขึ้นกล่องสีเขียวว่า "Data source is working")

**4.3 สร้าง Dashboard เพื่อดูกราฟ**

- ที่เมนูด้านซ้าย เลือกไอคอนเครื่องหมายบวก (+) > และคลิก **Dashboard**
- เลือก **Add visualization** และเลือก Data base Prometheus
- ที่ช่องกรอก Metrics / Query ลองใส่ Query ดังต่อไปนี้เพื่อแสดงข้อมูล Requests ต่อนาที:
  ```promql
  rate(http_requests_total[1m])
  ```
  _(สังเกตว่าชื่อ Metric ของระบบเราตอนนี้คือ `http_requests_total` จะต่างจากตัวอย่างในแลปที่คุณส่งมาที่ชื่อว่า `myapp_http_requests_total`)_
- กดปุ่ม **Run query**
- จะเห็นกราฟ Traffic ที่ดึงมาจากทั้ง 3 services (course-service, student-service, enrollment-service) แยกเป็นสีๆ โชว์ขึ้นมาบนหน้าจอ

_เพิ่มเติม Query ที่น่าสนใจ:_
ถ้าอยากดูกราฟเวลาการตอบสนองของเซิฟเวอร์ (Response Time) สามารถใช้ค่านี้แทน:

```promql
rate(http_request_duration_seconds_sum[1m])
```
```promql
rate(http_request_duration_seconds_count[1m])
```

### 5. เครื่องมือช่วยเหลืออื่นๆ

- 📈 **Prometheus UI**: `http://localhost:9090`
- 🐇 **RabbitMQ Management**: `http://localhost:15672` _(User: guest / Pass: guest)_
- 🟢 **Consul UI**: `http://localhost:8500`

### 6. การปิดการทำงานของระบบ

หลังทดสอบเสร็จแล้ว และต้องการลบการทำงานและล้างข้อมูลฐานข้อมูลปัจจุบัน ให้ใช้คำสั่ง:

```bash
docker compose down -v
```
