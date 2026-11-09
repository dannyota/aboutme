# Vietnam tech resume entries

This page holds the entry field rules of the
[Vietnam tech resumes](vietnam-tech-resumes.md) guide. **(judgment)** marks and
source names follow that guide; its [Sources](vietnam-tech-resumes.md#sources)
section lists the cited pages.

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

For freshers, keep one or two awards as bullets in `education`, beside the GPA
**(judgment)**. Count a recurring merit scholarship as one award and state the
number of semesters; a project is not an award. Give the prize's rank and scope:
`Giải Nhì cuộc thi lập trình sinh viên cấp trường` /
`Second prize, university student programming contest`. A custom `Giải thưởng` /
`Awards` section is an option for three or more distinct awards, or one whose
name matters to the target role, such as a national programming Olympiad prize
or an ICPC regional rank **(judgment)**. Keep only relevant awards; do not
repeat them under education.

A short graduation project or student research result can also be an education
bullet, with a score when available: `Đồ án tốt nghiệp (9,2/10): ...` /
`Graduation project (9.2/10): ...`. Put a project with a repository link under
`Dự án` / `Projects`, with its scope and result, instead of repeating it under
education. Both `minimal-air` variants show education bullets for a scholarship,
an unlinked graduation project, and a competition prize. A coding bootcamp is
its own `education` entry: the program in `degree`, such as
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
`link`. Leave out Facebook.

Zalo follows the language split **(judgment)**. In Vietnamese, include a
`custom` detail labelled `Zalo` when the candidate uses it and wants recruiter
contact there. Use `cùng số` only when it refers to the visible phone number;
otherwise give the number the candidate agrees to share. The English version
omits this channel hint to keep the header focused on phone, email, and
professional links. This is an editorial choice, not a claim that English
readers never use Zalo or that every Vietnamese candidate must use it.
Vietnamese tech samples model this choice with `Zalo: cùng số`.

Zalo's policy lists phone numbers as account information and describes contact
search using phone contacts with consent (Zalo). It does not establish recruiter
preferences or guarantee discovery by phone number. Omitting the Zalo hint does
not make the listed phone number private. Keep the `phone` value to digits,
spaces, and `+`, so it stays a tap-to-call link
([ADR 0013](../adr/0013-resume-header-and-contacts.md)).
