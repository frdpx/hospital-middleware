# Hospital Middleware — เอกสารวางแผนการพัฒนา

Repository: https://github.com/frdpx/hospital-middleware

เอกสารนี้ครอบคลุม deliverable ทั้ง 3 ส่วนที่โจทย์กำหนด ได้แก่
**Project Structure**, **API Spec** และ **ER Diagram** โดยเขียนให้อ่านจบได้ในตัวเอง
ไม่ต้องเปิดโค้ดประกอบ

---

## 1. ภาพรวมระบบ

Middleware ที่ทำหน้าที่เป็นตัวกลางระหว่างเจ้าหน้าที่โรงพยาบาลกับ Hospital
Information System (HIS) ของแต่ละโรงพยาบาล

- เจ้าหน้าที่สังกัดโรงพยาบาลเดียว และ login ผูกกับโรงพยาบาลนั้น
- เจ้าหน้าที่ค้นหาคนไข้ได้**เฉพาะของโรงพยาบาลตัวเองเท่านั้น**
- ข้อมูลคนไข้ดึงจาก HIS ของโรงพยาบาลนั้นเมื่อจำเป็น แล้ว normalize เก็บลง schema ของเราเอง

```
client ──► nginx ──► Go API ──► PostgreSQL
                        │
                        └──► HIS adapter ──► HIS ของโรงพยาบาลนั้น
```

**Tech stack (ตามที่โจทย์กำหนด):** Go · Gin · PostgreSQL · Docker + docker-compose · Nginx

### โจทย์ 2 ข้อที่กำหนดรูปร่างของการออกแบบ

**ก. คนคนเดียว อยู่ได้หลายโรงพยาบาล** — คนเดียวกันเป็นคนไข้ได้หลายโรงพยาบาล
และ HIS ของแต่ละที่จะออกเลข hospital number (`patient_hn`) ให้**คนละเลข**
ถ้าเก็บ "คนไข้" เป็นแถวเดียวต่อโรงพยาบาลแบบแบน ๆ จะไม่มีทางรู้เลยว่าคนไข้ของ
โรงพยาบาล A กับ B เป็นคนเดียวกัน

**ข. การแยกข้อมูลอย่างเด็ดขาด** — เจ้าหน้าที่ต้องไม่เห็นคนไข้ของโรงพยาบาลอื่น
ข้อนี้เป็น requirement ที่พลาดง่ายที่สุด จึงบังคับด้วย**รูปร่างของ schema และตัว
query เอง** ไม่ใช่ด้วยเงื่อนไข if ที่ใครสักคนอาจลืมเขียน

ทั้งสองข้อแก้ด้วยการแยกตารางคนไข้ออกเป็นสองตาราง — ดูหัวข้อ 4

---

## 2. โครงสร้างโปรเจกต์ (Project Structure)

### ผังโฟลเดอร์

```
.
├── .github/workflows/ci.yml     CI: fmt, vet, race tests, govulncheck,
│                                รัน migration บน Postgres จริง, build image
├── cmd/
│   ├── api/main.go              composition root — ไฟล์เดียวที่รู้จัก
│   │                            implementation จริงของทุก layer
│   ├── migrate/main.go          สั่ง apply/rollback migration แบบตั้งใจ
│   └── mockhis/main.go          ตัวแทน HIS ของ Hospital A (ใช้ตอน dev)
├── internal/
│   ├── apierr/                  error type เดียวของระบบ: HTTP status +
│   │                            ข้อความที่ปลอดภัยพอจะส่งให้ client + cause ที่ซ่อนไว้
│   ├── auth/                    ออกและตรวจสอบ JWT
│   ├── config/                  อ่าน env + validate ตอน startup
│   ├── db/                      connection pool, ตัวรัน migration ที่ฝังในไบนารี
│   ├── handler/                 Gin handler, request/response DTO, router
│   ├── hisclient/               HISClient interface + adapter ของ Hospital A
│   │   └── mockhis/             mock HIS ในรูป http.Handler ธรรมดา
│   ├── middleware/              auth, request id, access log, panic recovery
│   ├── models/                  domain type
│   ├── repository/              SQL ทุกคำสั่งในระบบอยู่ที่นี่ที่เดียว
│   ├── service/                 business rule ขึ้นกับ interface เท่านั้น
│   └── testutil/                test double: in-memory repo และ fake HIS
├── migrations/                  ไฟล์ SQL เรียงลำดับ ฝังเข้าไบนารี
├── deploy/nginx/                config ของ reverse proxy
├── docs/                        เอกสารนี้, api-spec, er-diagram, ADR
├── Dockerfile                   multi-stage แยก target api / mockhis
├── docker-compose.yml           nginx + api + postgres (+ mockhis ใน dev profile)
└── Makefile
```

### กฎเรื่องทิศทางของ dependency

```
handler  ──►  service  ──►  repository  ──►  PostgreSQL
   │             │
   │             └──────►  hisclient  ──►  HIS ภายนอก
   │
   └──►  middleware  ──►  auth
```

Dependency ชี้ไป**ทางเดียว**เท่านั้น `service` ไม่ import `handler`,
`repository` ไม่ import `service` มีเพียง `models` ที่เป็น leaf ร่วม
ซึ่งไม่ import อะไรของเราเลย

### Interface ถูกประกาศฝั่งผู้ใช้ ไม่ใช่ฝั่งผู้สร้าง

`service` ประกาศ `HospitalRepository`, `StaffRepository` และ
`PatientRepository` เป็น interface เล็ก ๆ **ตรงจุดที่ใช้งาน** ส่วน `repository`
คืน struct จริงและไม่รู้ด้วยซ้ำว่ามี interface พวกนี้อยู่ หลักการเดียวกันใช้กับ
`hisclient.HISClient`

ผลที่ได้ 2 ข้อ:

1. business rule ทุกข้อ unit test ได้ด้วย in-memory fake — ไม่ต้องมี database
   ไม่ต้องมี Docker ไม่ต้องมี test container ทั้ง suite รันจบในไม่กี่วินาที
2. การรองรับ **Hospital B** ทำได้โดยเพิ่ม `HISClient` หนึ่งตัวกับข้อมูลหนึ่งแถว
   ในตาราง `hospitals` ไม่ต้องแก้ schema ไม่ต้องแก้ service layer

### อะไรอยู่ที่ไหน

| Package | เก็บอะไร | **ไม่**เก็บอะไร |
|---|---|---|
| `handler` | binding, validate *รูปแบบ*, แปลง DTO ↔ model, render status | business rule, SQL |
| `service` | business rule, orchestration, validate *ความหมาย* | SQL, เรื่องของ HTTP |
| `repository` | SQL, การแปลความ error ของ driver | business rule |
| `hisclient` | รูปแบบ payload ของระบบต้นทาง, การ normalize | การจัดเก็บ, business rule |
| `models` | domain type และ invariant ของมัน | รูปแบบ JSON ของ API |

รูปแบบ JSON ของ API เป็น DTO ที่อยู่ใน `handler` แยกจาก `models` โดยตั้งใจ
การเปลี่ยนชื่อ column จึงไม่กลายเป็น breaking change ของ API แบบเงียบ ๆ
และ `models.Staff.PasswordHash` ก็หลุดออกไปไม่ได้เพราะมีคนเผลอ return model ตรง ๆ

### การจัดการ error — ทางเดียวเท่านั้น

ทุก error เดินทางไปหา client ด้วยเส้นทางเดียวกัน:

```
apierr.Validation("date_of_birth must be in YYYY-MM-DD format")
        │
        ▼
respondError(c, logger, err)
        │
        ├── status ≥ 500 → log พร้อม cause ที่ซ่อนไว้
        └── render เป็น {"error":{"code":…,"message":…}}
```

error ที่ไม่เคยถูกจัดประเภทจะกลายเป็น `500` แบบกลาง ๆ ทำให้ SQL หรือ URL ของ
ระบบต้นทางไม่มีทางหลุดไปถึง client ส่วน cause จะเดินทางไปที่ log เท่านั้น

### Configuration และ migration

config ทั้งหมดมาจาก environment variable (12-factor) และ **validate ตอน startup**
ถ้าไม่ได้ตั้ง `POSTGRES_PASSWORD`, ตั้ง `JWT_SECRET` สั้นเกินไป หรือใส่ค่าผิดรูปแบบ
อย่าง `JWT_TTL=1hour` process จะหยุดทันทีพร้อมแจ้งปัญหาทุกข้อในรอบเดียว

migration เป็นไฟล์ SQL ธรรมดา ฝังเข้าไบนารีด้วย `go:embed` และรันด้วย
`golang-migrate` ตอน startup ทำให้ image สมบูรณ์ในตัวเอง — โค้ดกับ schema ที่มัน
คาดหวังเดินทางไปด้วยกันเสมอ และ advisory lock ของ golang-migrate ทำให้ต่อให้ start
หลาย replica พร้อมกัน migration แต่ละตัวก็ยังถูก apply ครั้งเดียว

### กลยุทธ์การทดสอบ

| Layer | วิธี | พิสูจน์อะไร |
|---|---|---|
| `hisclient` | `httptest` + mock HIS ตัวจริง | contract กับ HIS, timeout, response ที่เพี้ยน |
| `service` | in-memory fake | business rule รวมถึงการแยกข้อมูลข้ามโรงพยาบาล |
| `handler` | Gin router เต็มตัวผ่าน `httptest` | middleware, binding, status code, error envelope |
| `repository` | `sqlmock` | ตัว SQL เอง: มี hospital filter จริง, ค่าถูก bind ไม่ใช่ต่อ string |
| `config`, `models`, `auth` | unit test ธรรมดา | validation, การจัดการวันที่, กฎของ token |

**รวม 210 test case** ผ่าน race detector ทั้งหมด
coverage ของ layer ที่สำคัญ: service 95%, handler 91%, repository 91%, auth 96%

---

## 3. API Spec

### ข้อตกลงร่วม

| | |
|---|---|
| Base URL | `http://localhost:8080` (ผ่าน nginx) |
| Content type | `application/json` ทั้ง request และ response |
| Auth | `Authorization: Bearer <jwt>` — บังคับเฉพาะ `/patient/search` |
| รูปแบบวันที่ | `YYYY-MM-DD` ทุกที่ |
| Correlation id | `X-Request-ID` ส่งกลับมาในทุก response |

**Error envelope** — response ที่ไม่ใช่ 2xx ใช้รูปแบบนี้เสมอ รวมถึง 404 จาก route
ที่ไม่มีอยู่ และ 429/503 ที่ nginx สร้างเอง client ควรตัดสินใจจาก `code`
ไม่ใช่จาก `message`

```json
{
  "error": {
    "code": "INVALID_CREDENTIALS",
    "message": "username, password or hospital is incorrect"
  }
}
```

**รายการ error code ทั้งหมด**

| Code | Status | ใช้ที่ไหน |
|---|---|---|
| `VALIDATION_ERROR` | 400 | ทุก endpoint |
| `UNAUTHORIZED` | 401 | `/patient/search` |
| `INVALID_CREDENTIALS` | 401 | `/staff/login` |
| `HOSPITAL_NOT_FOUND` | 404 | `/staff/create` |
| `PATIENT_NOT_FOUND` | 404 | `/patient/search` |
| `ROUTE_NOT_FOUND` | 404 | path ที่ไม่มีอยู่ |
| `METHOD_NOT_ALLOWED` | 405 | path ถูก แต่ method ผิด |
| `USERNAME_TAKEN` | 409 | `/staff/create` |
| `PATIENT_IDENTIFIER_CONFLICT` | 409 | `/patient/search` |
| `RATE_LIMITED` | 429 | ทุก endpoint (จาก nginx) |
| `INTERNAL_ERROR` | 500 | ทุก endpoint |
| `HIS_UNAVAILABLE` | 502 | `/patient/search` |
| `SERVICE_UNAVAILABLE` | 503 | ทุก endpoint (จาก nginx) |

---

### 3.1 `POST /staff/create`

สร้างบัญชีเจ้าหน้าที่ผูกกับโรงพยาบาลที่มีอยู่แล้ว **Auth:** ไม่ต้อง

**Request**

```json
{
  "username": "jsmith",
  "password": "P@ssw0rd123",
  "hospital": "hospital-a"
}
```

| Field | Type | บังคับ | หมายเหตุ |
|---|---|---|---|
| `username` | string | ใช่ | ยาวไม่เกิน 64 ตัว ไม่ซ้ำ**ภายในโรงพยาบาลเดียวกันเท่านั้น** |
| `password` | string | ใช่ | 8–72 ตัวอักษร เก็บเป็น bcrypt hash |
| `hospital` | string | ใช่ | ใส่ `code` หรือชื่อเต็มก็ได้ ไม่สนตัวพิมพ์เล็กใหญ่ |

**Response `201 Created`**

```json
{
  "id": "3f1b1e2a-9c3d-4e2a-8b1a-6a1c2d3e4f5a",
  "username": "jsmith",
  "hospital": "hospital-a",
  "created_at": "2026-07-31T09:00:00Z"
}
```

รหัสผ่านและ hash ของมันไม่ปรากฏใน response ใด ๆ ทั้งสิ้น

**Error**

| Status | Code | กรณี |
|---|---|---|
| 400 | `VALIDATION_ERROR` | ไม่ครบ field, password ไม่อยู่ในช่วง 8–72, JSON เพี้ยน |
| 404 | `HOSPITAL_NOT_FOUND` | ไม่มีโรงพยาบาลนี้ในตาราง `hospitals` |
| 409 | `USERNAME_TAKEN` | username นี้มีอยู่แล้ว**ในโรงพยาบาลนั้น** |

**เรื่องความยาว `password`:** bcrypt ตัดทิ้งเงียบ ๆ ที่ 72 byte การปฏิเสธตั้งแต่แรก
ปลอดภัยกว่าการรับรหัสผ่านที่ส่วนท้ายถูกเมิน

---

### 3.2 `POST /staff/login`

ตรวจสอบตัวตนแล้วคืน JWT ที่ผูก scope กับโรงพยาบาลของเจ้าหน้าที่คนนั้น
**Auth:** ไม่ต้อง

`hospital` เป็น field ที่จำเป็นจริง ไม่ใช่แค่ประดับ — เพราะ username ไม่ซ้ำเฉพาะ
ภายในโรงพยาบาล การค้นหาบัญชีจึงต้องใช้คู่ `(hospital, username)` เสมอ
ถ้าค้นด้วย username อย่างเดียวอาจไปเจอพนักงานของอีกโรงพยาบาลหนึ่ง

**Request**

```json
{
  "username": "jsmith",
  "password": "P@ssw0rd123",
  "hospital": "hospital-a"
}
```

**Response `200 OK`**

```json
{
  "access_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "token_type": "Bearer",
  "expires_in": 3600
}
```

JWT claims (HS256, อายุ default 1 ชั่วโมง):

```json
{
  "sub": "3f1b1e2a-9c3d-4e2a-8b1a-6a1c2d3e4f5a",
  "hospital_id": "8a2b1c3d-4e5f-4a7b-8c9d-0e1f2a3b4c5d",
  "username": "jsmith",
  "iss": "hospital-middleware",
  "iat": 1753868400,
  "exp": 1753872000
}
```

**Error**

| Status | Code | กรณี |
|---|---|---|
| 400 | `VALIDATION_ERROR` | ขาด field ที่จำเป็น |
| 401 | `INVALID_CREDENTIALS` | รหัสผ่านผิด, ไม่มี username นี้, ไม่มีโรงพยาบาลนี้, หรือ username นี้อยู่ที่โรงพยาบาล*อื่น* |

401 ทั้งสี่กรณีคืน body เหมือนกันเป๊ะและใช้เวลาใกล้เคียงกัน — กรณีไม่มี user
ก็ยังถูกนำไปเทียบกับ dummy bcrypt hash การตอบต่างกันจะเปิดช่องให้ผู้โจมตี
ไล่เดาว่ามีโรงพยาบาลไหนและ username ใดอยู่ในระบบบ้าง

---

### 3.3 `POST /patient/search`

ค้นหาคนไข้ จำกัดขอบเขตเฉพาะโรงพยาบาลที่ระบุอยู่ใน token **Auth:** บังคับ

ใช้ `POST` แทน `GET` เพราะ body มีเลขบัตรประชาชน เลขพาสปอร์ต และชื่อคนไข้
ซึ่งถ้าเป็น query string จะไปโผล่ใน nginx access log, ประวัติเบราว์เซอร์ และ proxy cache

**Request** — ทุก field เป็น optional แต่ต้องมีอย่างน้อยหนึ่งตัว
field ที่ส่งมาแต่เป็นค่าว่างจะถือว่าไม่ได้ส่ง

```json
{
  "national_id": "1234567890123",
  "passport_id": null,
  "first_name": "Somchai",
  "middle_name": null,
  "last_name": "Jaidee",
  "date_of_birth": "1990-05-20",
  "phone_number": null,
  "email": null
}
```

| Field | วิธี match |
|---|---|
| `national_id` | ตรงทั้งหมด |
| `passport_id` | ตรงทั้งหมด |
| `first_name` | substring ไม่สนตัวพิมพ์ เทียบกับ `first_name_th` **หรือ** `first_name_en`; ขั้นต่ำ 2 ตัวอักษร |
| `middle_name` | เหมือนข้างบน |
| `last_name` | เหมือนข้างบน |
| `date_of_birth` | ตรงทั้งหมด รูปแบบ `YYYY-MM-DD` |
| `phone_number` | ตรงทั้งหมด |
| `email` | ตรงทั้งหมด ไม่สนตัวพิมพ์ |

**ไม่มี field `hospital`** ขอบเขตมาจาก token ที่เซ็นแล้ว การส่ง `hospital`
มาใน body ไม่มีผลใด ๆ

**พฤติกรรม**

1. ค้นจากข้อมูลใน database ก่อน โดยกรองด้วย `hospital_id` จาก token เสมอ
2. ถ้าเจอ คืนผลลัพธ์
3. ถ้าไม่เจอ **และ** มีการค้นด้วย `national_id` หรือ `passport_id`
   **และ** เลขนั้นยังไม่เคยถูกบันทึกไว้สำหรับโรงพยาบาลนี้ → เรียก HIS ของโรงพยาบาลนั้น
   บันทึกผลลง (`patients` + `hospital_patients` ใน transaction เดียว)
   แล้วรัน query เดิมซ้ำและคืนผลลัพธ์
4. ถ้าไม่เจอ และค้นด้วยชื่อ/เบอร์/อีเมลเท่านั้น → คืน list ว่าง
   เพราะ HIS ไม่มี endpoint ให้ค้นด้วย field เหล่านี้

ผลที่ตามมาที่ควรรู้:

- ค้นด้วยชื่อแล้วไม่เจอ = `200 {"results":[],"count":0}` **ไม่ใช่** 404
- ค้นด้วยเลขประจำตัวแล้วไม่เจอทั้งใน database และที่ HIS = `404 PATIENT_NOT_FOUND`
- คนไข้ที่เป็นของโรงพยาบาลอื่นล้วน ๆ จะไม่ถูกคืนกลับมาไม่ว่าเส้นทางไหน
- ผลลัพธ์จำกัดที่ 100 แถว

**Response `200 OK`**

```json
{
  "results": [
    {
      "patient_hn": "HN00123",
      "national_id": "1234567890123",
      "passport_id": null,
      "first_name_th": "สมชาย",
      "middle_name_th": null,
      "last_name_th": "ใจดี",
      "first_name_en": "Somchai",
      "middle_name_en": null,
      "last_name_en": "Jaidee",
      "date_of_birth": "1990-05-20",
      "phone_number": "0812345678",
      "email": "somchai.jaidee@example.com",
      "gender": "M"
    }
  ],
  "count": 1
}
```

`patient_hn` คือเลข HN ที่**โรงพยาบาลนี้**ใช้ ถ้าคนเดียวกันถูกค้นจากอีกโรงพยาบาล
จะได้ `patient_hn` คนละเลข

**Error**

| Status | Code | กรณี |
|---|---|---|
| 400 | `VALIDATION_ERROR` | ไม่ส่ง field ใดเลย, `date_of_birth` ผิดรูปแบบ, ชื่อที่ค้นสั้นกว่า 2 ตัวอักษร |
| 401 | `UNAUTHORIZED` | ไม่มี token, token เพี้ยน, หมดอายุ, เซ็นด้วยกุญแจอื่น หรือไม่มี hospital scope |
| 404 | `PATIENT_NOT_FOUND` | ค้นด้วยเลขประจำตัวแล้วไม่เจอทั้งใน database และที่ HIS |
| 409 | `PATIENT_IDENTIFIER_CONFLICT` | HIS คืนเลขประจำตัวที่เป็นของคนไข้อีกคนอยู่แล้ว |
| 502 | `HIS_UNAVAILABLE` | HIS timeout, ปฏิเสธการเชื่อมต่อ, คืน 5xx หรือคืน body ที่ใช้ไม่ได้ |

---

### 3.4 ระบบต้นทาง: Hospital A HIS

```
GET https://hospital-a.api.co.th/patient/search/{id}
```

`{id}` คือ `national_id` หรือ `passport_id` field ที่เราใช้จาก response:
`first_name_th`, `middle_name_th`, `last_name_th`, `first_name_en`,
`middle_name_en`, `last_name_en`, `date_of_birth`, `patient_hn`, `national_id`,
`passport_id`, `phone_number`, `email`, `gender` (M/F)

Adapter จะ normalize ข้อมูลนี้เข้าสู่ model ภายในของเรา: string ว่างกลายเป็น
`NULL`, gender และวันที่ถูกจัดรูปแบบให้เป็นมาตรฐาน, เลขประจำตัวถูก escape
ก่อนต่อเข้า path และจำกัดขนาด response body ที่รับ

`404` จาก HIS แปลว่า "ไม่มีคนไข้คนนี้" (→ 404 ของเรา) ส่วน error อื่นทั้งหมด
แปลว่า "ระบบไม่พร้อมใช้งาน" (→ 502 ของเรา)

endpoint นี้เข้าถึงไม่ได้จากเครื่อง development โปรเจกต์จึงมี mock ที่ทำตาม
contract เดียวกัน ใช้ทั้งใน unit test และใน docker-compose profile `dev`

### 3.5 Endpoint สำหรับการดูแลระบบ

| Endpoint | หน้าที่ | เมื่อปกติ |
|---|---|---|
| `GET /healthz` | liveness — process ยังทำงานอยู่ | `200 {"status":"ok"}` |
| `GET /readyz` | readiness — ต่อ database ได้ | `200 {"status":"ready"}` / `503` |

---

## 4. ER Diagram

![ER diagram](assets/er-diagram.png)

*(ไฟล์รูป: `docs/assets/er-diagram.png` — ต้นฉบับ: `docs/assets/er-diagram.mmd`)*

### ตาราง

**`hospitals`** — reference data ที่ผู้ดูแลระบบเป็นคนเพิ่ม ไม่ได้สร้างผ่าน API

| Column | Type | หมายเหตุ |
|---|---|---|
| `id` | uuid | PK |
| `code` | text | unique; ใช้เป็นค่า `hospital` ใน staff API เช่น `hospital-a` |
| `name` | text | unique; ชื่อที่แสดงผล เช่น `Hospital A` |
| `his_adapter_type` | text | บอกว่าโรงพยาบาลนี้ใช้ `HISClient` ตัวไหน |
| `his_base_url` | text | nullable; endpoint ของ HIS โรงพยาบาลนั้น |
| `created_at`, `updated_at` | timestamptz | |

**`staff`** — บัญชีเจ้าหน้าที่ของโรงพยาบาล

| Column | Type | หมายเหตุ |
|---|---|---|
| `id` | uuid | PK |
| `hospital_id` | uuid | FK → `hospitals` |
| `username` | text | unique **ภายใน `hospital_id`** ไม่ใช่ทั้งระบบ |
| `password_hash` | text | bcrypt |
| `created_at`, `updated_at` | timestamptz | |

**`patients`** — ข้อมูลตัวตนของคน โดยไม่ผูกกับโรงพยาบาลใด

| Column | Type | หมายเหตุ |
|---|---|---|
| `id` | uuid | PK |
| `national_id` | text | nullable, unique เมื่อมีค่า |
| `passport_id` | text | nullable, unique เมื่อมีค่า |
| `first_name_th`, `last_name_th` | text | |
| `middle_name_th` | text | nullable |
| `first_name_en`, `last_name_en` | text | |
| `middle_name_en` | text | nullable |
| `date_of_birth` | date | nullable |
| `phone_number`, `email` | text | nullable |
| `gender` | text | `M`, `F` หรือค่าว่าง |
| `created_at`, `updated_at` | timestamptz | |

**`hospital_patients`** — เชื่อมคนหนึ่งคนเข้ากับโรงพยาบาลหนึ่งแห่ง พร้อมเลข HN ของที่นั่น

| Column | Type | หมายเหตุ |
|---|---|---|
| `id` | uuid | PK |
| `hospital_id` | uuid | FK → `hospitals` |
| `patient_id` | uuid | FK → `patients` |
| `patient_hn` | text | เลข HN ของโรงพยาบาลนั้น มาจาก HIS ของที่นั่น |
| `synced_at` | timestamptz | ครั้งล่าสุดที่ sync จาก HIS |
| `created_at` | timestamptz | |

### ทำไมต้องแยก `patients` ออกจาก `hospital_patients`

คนคนเดียวเป็นคนไข้ได้หลายโรงพยาบาล และ HIS ของแต่ละที่ออกเลข `patient_hn`
ให้คนละเลข ถ้าใช้ตารางเดียวที่ key ด้วย `(hospital_id, hn)` จะไม่มีทางรู้ว่า
"นี่คือคนเดียวกัน" ข้ามโรงพยาบาล และข้อมูลส่วนตัวจะถูกทำซ้ำแล้วค่อย ๆ เพี้ยน
ต่างกันไปในแต่ละโรงพยาบาล

การแยกทำให้แต่ละตารางมีหน้าที่เดียวชัดเจน:

| ตาราง | ตอบคำถามว่า |
|---|---|
| `patients` | *คนนี้คือใคร?* key ด้วยเลขที่ไม่ซ้ำกันทั้งประเทศ |
| `hospital_patients` | *โรงพยาบาลนี้รู้จักเขาไหม และเรียกเขาว่าอะไร?* |

และยังเปลี่ยนกฎการเข้าถึงข้อมูลหลักของโจทย์ให้กลายเป็นคุณสมบัติของ schema
`hospital_patients.hospital_id` คือ column ที่กำหนดขอบเขต และ**ทุก query
ของคนไข้เริ่มจากตาราง `hospital_patients` เสมอ**:

```sql
FROM hospital_patients hp
JOIN patients p ON p.id = hp.patient_id
WHERE hp.hospital_id = $1   -- มาจาก JWT มีเสมอ
  AND ...
```

แถวคนไข้ที่มีอยู่เฉพาะของโรงพยาบาลอื่น **คำสั่งนี้เอื้อมไปไม่ถึงตั้งแต่แรก**
ไม่มีเส้นทางไหนในโค้ด รวมถึงเส้นทาง HIS fallback ที่จะคืนข้อมูลนั้นออกมาได้

### ทำไม `staff.username` ไม่ซ้ำเฉพาะภายในโรงพยาบาล

โจทย์กำหนดให้ `/staff/login` รับ `hospital` เป็น input ซึ่งจะสมเหตุสมผลก็ต่อเมื่อ
`username` อย่างเดียวระบุตัวบัญชีไม่ได้ — แปลว่าสองโรงพยาบาลมี `jsmith`
คนละคนได้ บังคับด้วย composite unique index บน `(hospital_id, lower(username))`
และการค้นหาตอน login ใช้คู่นี้เสมอ

### ทำไม `hospitals` ต้องมี `his_adapter_type` และ `his_base_url`

โจทย์ระบุแค่ Hospital A แต่ถ้อยคำบ่งบอกว่าจะมี Hospital B, C ตามมา
โดยแต่ละที่มีรูปแบบ payload ของตัวเอง การเก็บว่าโรงพยาบาลไหนใช้ adapter ตัวไหน
ทำให้การรองรับ HIS ใหม่เหลือแค่: เขียน `HISClient` เพิ่มหนึ่งตัว เพิ่ม case
ใน factory แล้ว insert หนึ่งแถว

### Index และ constraint

| Index / constraint | เพื่ออะไร |
|---|---|
| `ux_hospitals_code`, `ux_hospitals_name` | รับค่า `hospital` ได้ทั้งสองรูปแบบ |
| `ux_staff_hospital_username` บน `(hospital_id, lower(username))` | username ไม่ซ้ำภายในโรงพยาบาล |
| `ix_staff_hospital_id` | ทุกการ login และการค้นหากรองด้วย hospital |
| `ux_patients_national_id` (partial) | หนึ่งคนต่อหนึ่งเลขบัตรประชาชน |
| `ux_patients_passport_id` (partial) | หนึ่งคนต่อหนึ่งเลขพาสปอร์ต |
| `ck_patients_has_identifier` | ต้องมีเลขบัตรประชาชน **หรือ** พาสปอร์ตอย่างน้อยหนึ่ง |
| `ck_patients_gender` | `M`, `F` หรือค่าว่าง |
| `ux_hospital_patients_hospital_patient` | คนไข้หนึ่งคนลงทะเบียนได้ครั้งเดียวต่อโรงพยาบาล |
| `ux_hospital_patients_hospital_hn` | เลข HN ไม่ซ้ำภายในโรงพยาบาล |
| `ix_patients_*` บน `lower(name)`, dob, phone, `lower(email)` | รองรับ filter ของการค้นหา |

การบังคับ unique บนเลขประจำตัวใช้ **partial index**
(`WHERE national_id IS NOT NULL`) คนไข้จำนวนมากจึงมี passport เป็น NULL ได้
ขณะที่เลขที่มีค่าจริงยังไม่ซ้ำกัน

---

## 5. Infrastructure

`docker-compose.yml` ประกอบด้วย service ที่โจทย์กำหนดครบ 3 ตัว
แต่ละตัวรอให้ dependency รายงานสถานะ **healthy** ไม่ใช่แค่ start แล้ว

| Service | Image | หน้าที่ |
|---|---|---|
| `nginx` | `nginx:1.27-alpine` | reverse proxy, rate limit, security header |
| `api` | build จาก `Dockerfile` (target `api`) | Go service |
| `postgres` | `postgres:16-alpine` | database, เปิด port เฉพาะ `127.0.0.1` |

มี profile เสริมชื่อ `dev` ที่เพิ่ม `mockhis` ซึ่งเป็นตัวแทนของ Hospital A
build จาก image target ของตัวเอง ทำให้ไม่มีตัวจำลองสำหรับ development
หลงเหลืออยู่ใน image ที่ใช้จริง

**nginx** จำกัดอัตราการเรียก endpoint ที่รับ credential ไว้ที่ 30 ครั้ง/นาที
และ API ที่เหลือ 60 ครั้ง/วินาที, จำกัด request body ที่ 64 KB, ใส่ security header,
ส่งต่อ `X-Request-ID` และ render 429/503 ของตัวเองด้วย JSON envelope
รูปแบบเดียวกับที่ API ใช้

**Image ของ Go** เป็น multi-stage build ได้ static binary ที่ไม่พึ่ง CGO
รันบน Alpine ด้วย user ที่ไม่ใช่ root และมี healthcheck ในตัว

### วิธีรัน

```bash
cp .env.example .env          # แล้วแก้ JWT_SECRET
docker compose --profile dev up -d --build
```

ทุกอย่างให้บริการผ่าน nginx ที่ `http://localhost:8080`

```bash
# 1. สร้างบัญชีเจ้าหน้าที่
curl -X POST http://localhost:8080/staff/create \
  -H 'Content-Type: application/json' \
  -d '{"username":"jsmith","password":"P@ssw0rd123","hospital":"hospital-a"}'

# 2. login
TOKEN=$(curl -s -X POST http://localhost:8080/staff/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"jsmith","password":"P@ssw0rd123","hospital":"hospital-a"}' \
  | jq -r .access_token)

# 3. ค้นหา — ยังไม่มีใน database จึงถูกดึงจาก HIS แล้วบันทึกไว้
curl -X POST http://localhost:8080/patient/search \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"national_id":"1234567890123"}'
```

### CI

GitHub Actions รันทุกครั้งที่ push: gofmt, `go vet`, test ทั้งหมดพร้อม race
detector และ coverage, `govulncheck`, รัน migration **ทั้งขาไปและขากลับ**
บน Postgres จริง, build image ทั้งสองตัว, `docker compose config` และ `nginx -t`

---

## 6. หมายเหตุด้านความปลอดภัย

- รหัสผ่านเก็บเป็น bcrypt hash และ field นี้ติด tag `json:"-"` ทำให้หลุดผ่าน
  handler โดยอุบัติเหตุไม่ได้
- การ login คืน `401` แบบเดียวกันสำหรับความล้มเหลวทุกรูปแบบ และใช้เวลาใกล้เคียงกัน
  endpoint นี้จึงไม่กลายเป็นเครื่องมือไล่เดาว่ามีบัญชีใดอยู่บ้าง
- ตัว parse JWT ล็อก algorithm ไว้ที่ HS256 ปิดช่องโจมตีแบบ `alg: none`
  และ hospital scope เป็น claim ที่ถูกเซ็น client จึงขยายขอบเขตตัวเองไม่ได้
- response ที่เป็น error ไม่มีทางมี SQL, URL ของระบบต้นทาง หรือรายละเอียดการเชื่อมต่อ
- access log ตั้งใจไม่บันทึก request body และ query string เพราะจะมีเลขบัตรประชาชน
  และชื่อคนไข้อยู่ในนั้น
- service รันด้วย user ที่ไม่ใช่ root ใน image แบบ static multi-stage
- **`/staff/create` ไม่ต้อง authenticate** เพื่อให้สาธิตระบบได้ครบวงจรจาก database
  เปล่า ๆ นี่เป็นจุดเดียวที่จงใจต่างจากสิ่งที่ production ควรเป็น
  ในระบบจริงควรต้องใช้ admin token หรือย้ายไปอยู่ใน admin surface ภายใน
- **stack นี้ไม่มี TLS** nginx รับ HTTP ธรรมดา โดยออกแบบให้อยู่หลัง
  load balancer หรือ ingress ที่ทำหน้าที่ terminate TLS

## 7. ข้อจำกัดที่ทราบ

- การค้นหาด้วยชื่อใช้ `ILIKE '%term%'` ซึ่งใช้ btree index ไม่ได้เพราะมี wildcard
  นำหน้า ที่ขนาดข้อมูลระดับนี้ยังไม่เป็นปัญหา ถ้าโตขึ้นให้เปลี่ยนไปใช้ `pg_trgm` GIN index
- `synced_at` ถูกบันทึกไว้แล้วแต่ยังไม่ได้ใช้ตัดสินใจ re-fetch ข้อมูลที่เก่า
  ปัจจุบันเรียก HIS เฉพาะตอนที่ยัง*ไม่มี*เลขประจำตัวนั้นของโรงพยาบาลนี้
- การ sync จาก HIS เป็นการ merge เข้ากับข้อมูลเดิม จึงไม่สามารถล้างค่า field ได้
  ค่าที่ถูกลบจริงที่ต้นทางจะยังคงอยู่จนกว่าผู้ดูแลระบบจะลบเอง — การเก็บค่าเก่าไว้
  ปลอดภัยกว่าปล่อยให้ HIS ที่มีข้อมูลน้อยกว่าลบข้อมูลที่ดีทิ้ง
- ยังไม่มี refresh token, token หมดอายุแล้วต้อง login ใหม่
- การค้นหายังไม่มี pagination จำกัดผลลัพธ์ไว้ที่ 100 แถว

---

## ภาคผนวก — บันทึกการตัดสินใจ (ADR)

เหตุผลฉบับเต็มของการตัดสินใจข้างต้นอยู่ใน `docs/adr/`:

| # | การตัดสินใจ |
|---|---|
| 0001 | ใช้ layered monolith พร้อม interface ที่ประกาศฝั่งผู้ใช้ |
| 0002 | แยก `patients` ออกจาก `hospital_patients` |
| 0003 | ปล่อยให้ `/staff/create` ไม่ต้อง authenticate (พร้อมข้อควรระวังที่ระบุไว้) |
| 0004 | hospital scope อยู่ใน JWT ไม่ใช่ใน request |
| 0005 | migration ฝังในไบนารีและ apply ตอน API startup |
| 0006 | `/patient/search` ใช้ POST ไม่ใช่ GET |
| 0007 | ค้นในเครื่องก่อน เรียก HIS เฉพาะการค้นด้วยเลขประจำตัว |
