# Vietnam tech resumes

This guide sets the language rules for resumes written by software developers in
Vietnam. The gallery samples, template reviews, and any writing help in aboutme
follow it. It covers headings, which words stay in English, bullets, dates,
local credentials, the choice of resume language, and what an applicant tracking
system (ATS) reads.

Facts about certificate scales and degree rules cite a source in
[Sources](#sources). A claim about hiring practice marked **(judgment)** is the
author's reading of the Vietnamese market, not a cited fact.

## Choose the resume language first

Write one resume per language. Do not mix a Vietnamese and an English version on
one page. aboutme stores one resume language per resume (`metadata.lng`), and
the renderer uses it for the date words and public page labels. It never
translates content. An account holds up to three resumes, so a Vietnamese and an
English version use two of them.

| Situation                                                      | Write                                                 |
| -------------------------------------------------------------- | ----------------------------------------------------- |
| Product company whose job post is in English                   | Fully English                                         |
| Foreign employer, remote role, or a team that works in English | Fully English                                         |
| Local company or SME whose job post is in Vietnamese           | Vietnamese with English terms                         |
| State-owned or government-linked employer, state bank          | Vietnamese with English terms                         |
| Japan-facing outsourcing company or BrSE role                  | Match the job post; list the JLPT level in `language` |

The simplest rule is to match the language of the job post **(judgment)**. A
Japanese-format resume (履歴書, rirekisho) is out of scope for this guide.

## Section headings

Use the common headings below. Recruiters scan for them, and an ATS looks for
them. Each maps to one schema section type; the heading is the section's
`displayName`.

| Vietnamese            | English        | Schema `sectionType` | Use                                           |
| --------------------- | -------------- | -------------------- | --------------------------------------------- |
| Giới thiệu or Tóm tắt | Summary        | `profile`            | Two or three lines: role, years, strengths    |
| Mục tiêu nghề nghiệp  | Objective      | `profile`            | Freshers and interns only                     |
| Kinh nghiệm làm việc  | Experience     | `work`               | Jobs, newest first                            |
| Dự án                 | Projects       | `project`            | Side, open source, or graduation projects     |
| Kỹ năng               | Skills         | `skill`              | Exact technology names                        |
| Học vấn               | Education      | `education`          | Degree and school                             |
| Chứng chỉ             | Certifications | `certificate`        | Cloud, testing, security, and PM certificates |
| Ngoại ngữ             | Languages      | `language`           | English and Japanese with scores              |
| Giải thưởng           | Awards         | `custom`             | Competitions and scholarships                 |
| Hoạt động             | Activities     | `custom`             | Clubs, volunteering, talks                    |

Contact data is not a section. Name, headline, email, phone, city, GitHub,
LinkedIn, and website go in `personalDetails`.

Good: `Kinh nghiệm làm việc`, `Experience`.

Bad: `Hành trình sự nghiệp của tôi`, `My Journey`, `★ KINH NGHIỆM ★`. Invented
or decorated headings slow the reader and can hide the section from an ATS.

Always leave out gender, marital status, hometown, and ID number (CCCD). Date of
birth and a photo follow the language split in
[Choose the resume language first](#choose-the-resume-language-first). Leave
both out for a product company, a foreign employer, or an English resume. A
local company, SME, bank, or state employer may expect a plain headshot and a
birth year, and a resume without them can read as incomplete there
**(judgment)**. The schema has no date-of-birth field; a person who wants one
adds it as a `custom` contact detail.

## English inside a Vietnamese resume

Vietnamese developers read and speak technology in English. A Vietnamese resume
that translates every term reads as stiff, and it breaks keyword search
**(judgment)**. Keep the Vietnamese sentence and keep the English term.

### Keep in English

- **Job titles:** Backend Developer, Senior Software Engineer, DevOps Engineer,
  QA Engineer, Automation Tester, BrSE, Tech Lead, Engineering Manager.
- **Levels:** Intern, Fresher, Junior, Middle, Senior. "Middle" is common in
  Vietnamese job posts but rare abroad; in an English resume for a foreign
  employer, write the title without it **(judgment)**.
- **Languages and frameworks:** Go, Java, Python, TypeScript, Node.js, React,
  Vue.js, Next.js, Spring Boot, .NET, Flutter, Kotlin, Swift.
- **Data, cloud, and tooling:** PostgreSQL, MySQL, Redis, Kafka, Docker,
  Kubernetes, Terraform, AWS, Google Cloud, Azure, GitHub Actions, GitLab CI,
  Jira.
- **Practice names:** CI/CD, REST API, gRPC, microservices, unit test, code
  review, design system, A/B test, on-call, Scrum, Agile.
- **Work nouns developers say in English:** deploy, refactor, release, hotfix,
  scale, cache, request, latency, sprint, backlog.

Write each name exactly as its project writes it: `Node.js`, not `NodeJS` or
`node`; `PostgreSQL`, not `Postgre`; `JavaScript`, not `Javascript`;
`Kubernetes (K8s)` once, then either form.

### Write in Vietnamese

- The sentence: verbs, connectors, and the result.
- Common action verbs that have a natural Vietnamese word: thiết kế, xây dựng,
  phát triển, triển khai, tối ưu, tự động hóa, chuyển đổi, dẫn dắt, kèm cặp,
  giảm, tăng, rút ngắn.
- The business domain: thanh toán, thương mại điện tử, ngân hàng số, bảo hiểm,
  logistics (loan word, keep it).
- Units and counts: triệu, nghìn, người dùng, giao dịch, ngày, tháng.
- Numbers use Vietnamese separators: `10.000` for ten thousand and `1,9 giây`
  for a decimal. English uses `10,000` and `1.9 s`.

Good:
`Thiết kế và triển khai microservices bằng Go, xử lý 5 triệu request/ngày.`

Bad, over-translated:
`Thiết kế và triển khai các dịch vụ vi mô bằng ngôn ngữ Go, xử lý 5 triệu yêu cầu mỗi ngày.`
"Dịch vụ vi mô" is correct Vietnamese, but readers and search tools look for
"microservices" **(judgment)**.

Bad, English with Vietnamese glue:
`Em đã làm deploy app lên server và fix bug cho team.` It uses a personal
pronoun, names no system, and has no result.

Start each bullet with the verb. Do not use `tôi`, `em`, or `I` as the subject.

## Job titles and headlines

The headline under the name holds the title and three or four core skills.

| Good (Vietnamese resume)                  | Good (English resume)                     |
| ----------------------------------------- | ----------------------------------------- |
| Backend Developer · Go, PostgreSQL, Kafka | Backend Developer · Go, PostgreSQL, Kafka |
| Senior Frontend Engineer · React, Next.js | Senior Frontend Engineer · React, Next.js |
| BrSE · JLPT N2, Java, dự án Nhật Bản      | Bridge Software Engineer · JLPT N2, Java  |
| Fresher Java Developer · Spring Boot      | Entry-level Java Developer · Spring Boot  |

Keep the level the same in both versions. `Fresher` becomes `Entry-level`, never
`Junior`, because a different level is a different claim.

Bad: `Kỹ sư Frontend cấp cao`. A recruiter searching "Senior Frontend" does not
find it, and Vietnamese job posts use the English title **(judgment)**.

Bad: `Lập trình viên đam mê công nghệ, ham học hỏi`. It says nothing a reader
can check.

Vietnamese words stay right for titles outside engineering and for roles at
state employers that use official Vietnamese titles, such as
`Chuyên viên Công nghệ thông tin` **(judgment)**.

## Accomplishment bullets

Each bullet is one achievement: **verb + scope + technology + result**. Put a
number in the result when the number is true and can be shared. Keep three to
five bullets per job, one or two lines each.

### Vietnamese patterns

| Pattern                                           | Example                                                                                      |
| ------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `<Động từ> <hệ thống> bằng <công nghệ>, <quy mô>` | Thiết kế và triển khai microservices bằng Go, xử lý 5 triệu request/ngày.                    |
| `<Động từ> ..., giảm <chỉ số> từ A xuống B`       | Tối ưu truy vấn PostgreSQL, giảm p99 latency từ 800 ms xuống 120 ms.                         |
| `<Động từ> ..., tăng/rút ngắn <chỉ số> X%`        | Xây dựng pipeline CI/CD với GitHub Actions, rút ngắn thời gian release từ 2 giờ còn 15 phút. |
| `Dẫn dắt nhóm N người <làm gì>, <kết quả>`        | Dẫn dắt nhóm 4 người chuyển hệ thống thanh toán lên Kubernetes, không gây downtime.          |
| `Kèm cặp N <ai>; <kết quả>`                       | Kèm cặp 3 fresher; cả 3 làm việc độc lập được sau 2 tháng.                                   |

### English patterns

Use past tense for past jobs and present tense for the current job.

- Designed and deployed Go microservices handling 5 million requests a day.
- Cut p99 latency from 800 ms to 120 ms by rewriting PostgreSQL queries.
- Built a GitHub Actions CI/CD pipeline that took releases from 2 hours to 15
  minutes.
- Led a team of 4 moving the payment system to Kubernetes with zero downtime.

### Bad bullets

| Bad                                             | Why                               |
| ----------------------------------------------- | --------------------------------- |
| Tham gia phát triển dự án.                      | No system, no role, no result     |
| Responsible for backend development.            | A duty, not an achievement        |
| Làm việc với nhiều công nghệ mới.               | Names nothing a reader can search |
| Tăng hiệu suất hệ thống 300%.                   | No baseline; reads as invented    |
| Sử dụng Java, Spring, MySQL, Redis, Kafka, AWS. | A skills list inside a job        |

When numbers are confidential, give a range or a relative change that the person
can defend in an interview: `hơn 1 triệu người dùng`,
`giảm khoảng 40% chi phí AWS`. Never invent a number.

For outsourcing work, name the client by domain and market, not by name:
`Dự án hệ thống bảo hiểm cho khách hàng Nhật Bản`,
`Insurance platform for a Japanese client`.

An outsourcing job with several client projects stays one `work` entry for the
employer. Each project gets its own bullet with domain, market, length, and
result: `Dự án ngân hàng số cho khách hàng Nhật Bản (8 tháng): ...`,
`Logistics platform for a US client (1 year): ...`. One entry per client reads
as job-hopping **(judgment)**.

Freelance work is one `work` entry with `employer` set to `Freelance` or
`Tự do`. Each engagement gets a bullet in the same form:
`Xây dựng hệ thống đặt lịch cho một phòng khám tư nhân (3 tháng), ...`.

## Dates

The renderer prints dates from the date fields, so never type them into the
description. Set the resume's date format to match its language:

| Resume language | Date format | Renders as         |
| --------------- | ----------- | ------------------ |
| Vietnamese      | `MM/YYYY`   | 03/2022 – Hiện tại |
| English         | `Mon YYYY`  | Mar 2022 – Present |

`Mon YYYY` on a Vietnamese resume prints `thg 3 2022`, which is correct but less
common on Vietnamese resumes than `03/2022` **(judgment)**. A current job sets
`present`; the renderer writes `Hiện tại` or `Present` from the resume language.
Use `YYYY` only for education on a long career.

Bad: `Từ tháng 3 năm 2022 đến nay`, `2022-03 ~ now`, `3/22 - nay`.

## Locations

Write the city, then the country.

| Vietnamese                | English                   |
| ------------------------- | ------------------------- |
| Hà Nội, Việt Nam          | Hanoi, Vietnam            |
| TP. Hồ Chí Minh, Việt Nam | Ho Chi Minh City, Vietnam |
| Đà Nẵng, Việt Nam         | Da Nang, Vietnam          |
| Làm việc từ xa            | Remote                    |

In the contact line, give the city or district only, such as `Cầu Giấy, Hà Nội`.
A full street address adds nothing and exposes the person **(judgment)**. For a
remote role with a foreign employer, add the time zone:
`Ho Chi Minh City, Vietnam (UTC+7)` **(judgment)**.

## Employers

Use the name people know. The legal form (`Công ty CP`, `Công ty TNHH`, `JSC`,
`Co., Ltd.`) is optional.

When the employer is confidential, or in a sample, describe it in the `employer`
field:

| Vietnamese                                   | English                                      |
| -------------------------------------------- | -------------------------------------------- |
| Công ty fintech khởi nghiệp                  | A fintech startup                            |
| Công ty thương mại điện tử (500 nhân sự)     | An e-commerce company (500 staff)            |
| Công ty outsourcing phần mềm thị trường Nhật | A Japan-focused software outsourcing company |
| Ngân hàng thương mại cổ phần                 | A joint-stock commercial bank                |

Bad: `Công ty X`, `Confidential`, `***`. These tell the reader nothing about
scale or domain.

A job shorter than a year, or a gap longer than three months, gets a short and
honest reason in that entry: `(hợp đồng dự án 6 tháng)`,
`(6-month project contract)`, `(công ty tái cơ cấu)`. Local recruiters screen
hard for job-hopping (nhảy việc), and an unexplained short stint often ends the
screen **(judgment)**.

## Languages and scores

Put the test and score in the entry `name`. The `level` field draws a visual
scale that an ATS does not read.

| Good (Vietnamese)     | Good (English)      |
| --------------------- | ------------------- |
| Tiếng Anh (IELTS 7.0) | English (IELTS 7.0) |
| Tiếng Anh (TOEIC 850) | English (TOEIC 850) |
| Tiếng Nhật (JLPT N2)  | Japanese (JLPT N2)  |

Scales, from the test owners:

- **TOEIC Listening and Reading** totals 10 to 990 points (ETS).
- **IELTS** reports bands from 0 to 9 in whole and half bands (IELTS).
- **JLPT** has five levels; N5 is the easiest and N1 the hardest (JLPT).

Bad: `Tiếng Anh: Khá`, `English: Good`. Give a score when one exists. Without a
test, describe use:
`Tiếng Anh (đọc tài liệu kỹ thuật, họp hằng tuần với khách hàng Singapore)`.

Japanese matters for Japan-facing outsourcing and BrSE roles. BrSE (Bridge
System Engineer or Bridge Software Engineer) is the developer who carries specs
and questions between a Japanese client and a Vietnamese team. For these roles,
JLPT N2 or N1 is the usual bar **(judgment)**. State the level honestly:
`JLPT N3, đang ôn N2` is better than an unexplained `Tiếng Nhật: tốt`.

## Education

The degree and the school are separate fields. Write the degree type, then the
major.

| Field    | Vietnamese                | English                                           |
| -------- | ------------------------- | ------------------------------------------------- |
| `degree` | Kỹ sư Công nghệ Thông tin | Bachelor of Engineering in Information Technology |
| `school` | Đại học Bách khoa Hà Nội  | Hanoi University of Science and Technology        |
| `degree` | Cử nhân Khoa học Máy tính | Bachelor of Science in Computer Science           |

In English, use the school's official English name, as printed on its website.
`Kỹ sư` programs run longer than `Cử nhân` programs; `Bachelor of Engineering`
is a common English rendering, and a transcript explains the rest
**(judgment)**.

Freshers and interns list the GPA with its scale: `GPA 3.4/4` or `GPA 8.2/10`.
Vietnamese universities on the 4-point scale classify graduates as Xuất sắc (3.6
to 4.0), Giỏi (3.2 to 3.59), Khá (2.5 to 3.19), and Trung bình (2.0 to 2.49)
under Circular 08/2021/TT-BGDĐT (Thông tư 08/2021/TT-BGDĐT). In English, give
the number, not a translated rank. List the GPA when it is Khá or better. Drop
it after about two years of work for a product company or foreign employer; keep
it for a bank or state employer, which often still asks **(judgment)**.

Freshers also list the graduation project (đồ án tốt nghiệp), student research,
scholarships, and competitions, in `Dự án` or a custom `Giải thưởng` section. A
coding bootcamp is its own `education` entry: the program in `degree`, such as
`Full-stack Web Development`, and the provider in `school`.

## Certifications and links

Write each certificate's official English title, even in a Vietnamese resume:
`AWS Certified Solutions Architect – Associate`,
`Certified Kubernetes Administrator (CKA)`,
`ISTQB Certified Tester Foundation Level`, `OSCP`. Put the issuer in `issuer`
and the month in `date`. Online course completions carry less weight than
proctored exams; list them only when they fill a real gap **(judgment)**. For a
fresher with no work history, an online certificate that matches the target
stack fills that gap; drop it once a job or a stronger project replaces it.

Links go in `personalDetails` with their types: `github`, `linkedin`, `website`.
Link a GitHub profile only when its pinned repositories show real work
**(judgment)**. A personal blog or talk slides go under `website` or a project
`link`. Leave out Facebook. Many local recruiters message candidates on Zalo
first **(judgment)**, so say which number takes Zalo in a `custom` detail, such
as `Zalo: cùng số` or `Zalo: same number`. Keep the `phone` value to digits,
spaces, and `+`, so it stays a tap-to-call link
([ADR 0043](../adr/0043-email-and-phone-links.md)).

## What an ATS reads

The template page's "What an ATS reads" tab ("ATS đọc được gì") shows the plain
text a parser gets from a sample: the header, then the main column, then the
sidebar, with each heading in capitals. It keeps names and text. It drops the
skill and language `level` graphics, icons, colors, and the photo.

- Use the headings in [Section headings](#section-headings).
- Put every skill in text. A skill level bar with no name means nothing to a
  parser.
- Spell keywords exactly as job posts do: `Node.js`, `PostgreSQL`, `Kubernetes`,
  `CI/CD`, `React Native`. Repeat the main ones inside bullets, where they show
  real use.
- Keep full Vietnamese diacritics. Text without diacritics reads as careless,
  and a search for `Hà Nội` may not match `Ha Noi` **(judgment)**.
- Do not use emoji, symbol bullets typed as text, or decorative capitals as
  words.
- Keep dates in date fields and contact data in contact fields.

Good skill entries: `Go`, `PostgreSQL`, `Kafka`, `Docker`, `Kubernetes`.

If a template shows skill levels, set them from 1 (used in one project) to 5
(daily use, could mentor others), or leave the level empty rather than guess
**(judgment)**.

Bad skill entries: `Backend (Go/Java/Node...)`, `Database các loại`,
`Kỹ năng làm việc nhóm` in the technical list.

## Writing the role samples

This section is for gallery sample authors; a person writing their own resume
can skip it. Every gallery sample for a tech role follows the rules above and
this checklist. The nine roles are Backend, Frontend, Mobile, DevOps/SRE,
Data/AI, QA/Tester, Fresher/Intern, BrSE, and Security.

### Checklist

- [ ] The person is fictional, with a common Vietnamese name. Email uses
      `example.com`, phones use `+84 90 000 0xxx`, and links use
      `https://example.com/...`.
- [ ] Employers and clients are neutral descriptions, such as
      `Công ty fintech khởi nghiệp` or `A fintech startup`. No real company,
      product, or client name.
- [ ] Schools are fictional, matching the existing samples.
- [ ] The career path is realistic for Vietnam: titles, years, and moves that a
      recruiter would believe (see the table below).
- [ ] Every bullet has a verb, a scope, and a believable number.
- [ ] The Vietnamese version keeps English titles and tech terms, uses
      `MM/YYYY`, and has full diacritics.
- [ ] The English version is written in English, not translated word by word,
      and uses `Mon YYYY`.
- [ ] Both versions state the same facts and fit one A4 page in the paired
      template.
- [ ] Headings come from the heading table.
- [ ] Skills are 6 to 10 exact keywords.
- [ ] Languages carry a test score or a concrete use.
- [ ] The template page's "What an ATS reads" tab shows every heading, title,
      date, and skill.

### Role notes

| Role           | Typical path (years of work)                                             | Core keywords                          | Metrics that fit                                  |
| -------------- | ------------------------------------------------------------------------ | -------------------------------------- | ------------------------------------------------- |
| Backend        | Fresher → Junior (1) → Middle (3) → Senior (5)                           | Go or Java, PostgreSQL, Redis, Kafka   | Requests a day, p99 latency, uptime               |
| Frontend       | Fresher → Junior (1) → Middle (3) → Senior (5)                           | React, TypeScript, Next.js, Playwright | LCP, bundle size, conversion rate                 |
| Mobile         | Fresher → Junior (1) → Senior (4)                                        | Flutter or Kotlin and Swift, Firebase  | Crash-free sessions, store rating, monthly users  |
| DevOps/SRE     | Backend or sysadmin → DevOps (2) → Senior DevOps Engineer (5)            | Kubernetes, Terraform, AWS, Prometheus | Deploys a week, MTTR, cloud cost                  |
| Data/AI        | Data Analyst → Data Engineer or ML Engineer (3)                          | Python, SQL, Spark, Airflow, PyTorch   | Rows a day, model accuracy, hours saved           |
| QA/Tester      | Manual Tester → Automation Tester (2) → QA Lead (5)                      | Selenium or Playwright, Postman, ISTQB | Automated cases, regression time, escaped defects |
| Fresher/Intern | Final-year student or new graduate                                       | One stack, Git, SQL                    | GPA, graduation project, internship output        |
| BrSE           | Developer (2) → BrSE (4), after study or work in Japan or Japan projects | JLPT N2, Java or .NET, spec writing    | Team size, change requests, UAT defects           |
| Security       | Developer or SOC analyst → Security Engineer (3)                         | OWASP, Burp Suite, SIEM, OSCP          | Findings fixed, time to patch, audits passed      |

The years and paths in this table are typical ranges, not rules **(judgment)**.
The SRE title appears mainly at large product companies **(judgment)**. MTTR
means mean time to recovery. UAT means user acceptance testing. SOC means
security operations center.

## Sources

- ETS Global, TOEIC Listening and Reading test:
  <https://www.etsglobal.org/ie/en/test-type-family/toeic-listening-and-reading-test>
- IELTS, scoring in detail:
  <https://ielts.org/take-a-test/your-results/ielts-scoring-in-detail>
- JLPT, N1 to N5 level summary: <https://www.jlpt.jp/e/about/levelsummary.html>
- Thông tư 08/2021/TT-BGDĐT, graduation classification on the 4-point scale:
  <https://thuvienphapluat.vn/lao-dong-tien-luong/xep-loai-tot-nghiep-dai-hoc-thang-diem-4-nhu-the-nao-tot-nghiep-dai-hoc-loai-xuat-sac-co-duoc-tuyen-33523.html>
