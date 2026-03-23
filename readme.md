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

docker compose up -d --build
```

### 3. ตรวจสอบพอร์ตและการทำงาน

เมื่อคอนเทนเนอร์ทั้งหมดทำงาน สามารถเข้าถึงเซอร์วิสต่างๆ ได้ตามพอร์ตด้านล่าง:

- 🌐 **Course Service Endpoint**: `http://localhost:8000`
- 🌐 **Student Service Endpoint**: `http://localhost:8001`
- 🌐 **Enrollment Service Endpoint**: `http://localhost:8002`

เครื่องมือช่วยเหลืออื่นๆ (Tools / UI):

- 📊 **Grafana Dashboard**: `http://localhost:3000` _(User: admin / Pass: admin)_
- 📈 **Prometheus UI**: `http://localhost:9090`
- 🐇 **RabbitMQ Management**: `http://localhost:15672` _(User: guest / Pass: guest)_
- 🟢 **Consul UI**: `http://localhost:8500`

### 4. ทดสอบใช้งานส่งคำสั่ง API

หลังจากระบบเริ่มต้นสำเร็จ (รวมถึงจัดการ Seed Database ของ Postgres เรียบร้อยแล้ว) สามารถทดสอบยิง API:

### 5. การปิดการทำงานของระบบ

หากทดสอบเสร็จแล้ว และต้องการลบการทำงานและล้างข้อมูลฐานข้อมูลปัจจุบัน ให้ใช้คำสั่ง:

```bash
docker compose down -v
```
