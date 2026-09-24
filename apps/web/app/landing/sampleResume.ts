import type { Resume } from '@aboutme/schema';

/** The compiled-in document shown on the public landing page. */
export const sampleResume: Resume = {
  schemaVersion: 4,
  personalDetails: {
    fullName: 'Danny',
    headline: 'Software Developer',
    photo: {
      key: 'landing/danny.jpg',
    },
    details: [
      {
        id: 'c652f275-ca24-4f52-9790-2149d67b27fc',
        type: 'location',
        value: 'Vietnam',
        isHidden: false,
      },
      {
        id: 'a3145d5b-6dce-4e8c-b01c-8a7ae7757298',
        type: 'github',
        value: 'https://github.com/dannyota',
        isHidden: false,
        display: 'label',
      },
    ],
  },
  content: {
    profile: {
      sectionType: 'profile',
      displayName: 'Summary',
      iconKey: 'user',
      entries: [
        {
          id: 'bac02090-1402-4141-80b0-82f3ef821def',
          isHidden: false,
          text:
            '<p>Developer who builds command-line tools, SDKs, and MCP '
            + 'servers in Go. Nine years in security engineering shaped how '
            + 'I write software: every change is a reviewed diff, dry-run '
            + 'by default, and tested in CI. I build and run aboutme.vn, a '
            + 'resume builder for Vietnamese job seekers.</p>',
        },
      ],
    },
    work: {
      sectionType: 'work',
      displayName: 'Experience',
      iconKey: 'briefcase',
      entries: [
        {
          id: '475659ae-1277-4a52-b456-87c58a185991',
          isHidden: false,
          jobTitle: 'Head of IT Security',
          employer: 'A regulated bank',
          employerLink: '',
          dates: {
            start: {
              y: 2025,
              m: 7,
            },
            end: {
              y: 2026,
              m: 8,
            },
            present: false,
          },
          description:
            '<ul><li>Built the security team\'s tooling in Go: CLIs and '
            + 'MCP servers that let a two-person team run operations and '
            + 'governance.</li><li>Designed an AI agent harness that works '
            + 'through reviewed, dry-run-first commands, with a person '
            + 'approving every change.</li></ul>',
        },
        {
          id: '8c69bc76-e4c4-41e3-99b6-c6e218207c3c',
          isHidden: false,
          jobTitle: 'Security Operations Engineer',
          employer: 'A Web3 gaming company',
          employerLink: '',
          dates: {
            start: {
              y: 2024,
              m: 4,
            },
            end: {
              y: 2025,
              m: 6,
            },
            present: false,
          },
          description:
            '<ul><li>Wrote custom detection rules and automated incident '
            + 'response with SOAR playbooks.</li><li>Reviewed third-party '
            + 'game builds before release with reverse engineering and '
            + 'automated scanning.</li></ul>',
        },
        {
          id: '7983cdd0-920a-40f1-a756-bc85ecf62fc7',
          isHidden: false,
          jobTitle: 'IT Security Engineer',
          employer: 'A semiconductor company',
          employerLink: '',
          dates: {
            start: {
              y: 2023,
              m: 5,
            },
            end: {
              y: 2024,
              m: 4,
            },
            present: false,
          },
          description:
            '<ul><li>Built an internal asset-management system that '
            + 'merged device and software inventories into one source of '
            + 'truth.</li></ul>',
        },
      ],
    },
    education: {
      sectionType: 'education',
      displayName: 'Education',
      iconKey: 'graduation-cap',
      entries: [
        {
          id: 'b80ad022-2fb7-40f9-8a4a-b7a00d658db1',
          isHidden: false,
          degree: 'Master of Science in Computer Science',
          school: 'University of Information Technology, VNU-HCM',
          schoolLink: '',
          dates: {
            start: {
              y: 2019,
              m: 12,
            },
            end: {
              y: 2022,
              m: 11,
            },
            present: false,
          },
        },
        {
          id: 'af09d414-cc1d-4865-b465-e7dad48bbf79',
          isHidden: false,
          degree: 'Bachelor of Engineering in Software Engineering',
          school: 'University of Information Technology, VNU-HCM',
          schoolLink: '',
          dates: {
            start: {
              y: 2014,
              m: 8,
            },
            end: {
              y: 2019,
              m: 5,
            },
            present: false,
          },
        },
      ],
    },
    skill: {
      sectionType: 'skill',
      displayName: 'Skills',
      iconKey: 'code',
      entries: [
        {
          id: '02877920-888c-4ae8-b836-119534f646b3',
          isHidden: false,
          name: 'Go',
          level: 5,
          infoHtml: '',
        },
        {
          id: 'cae9602e-16a2-409e-9112-251e5f404722',
          isHidden: false,
          name: 'Python',
          level: 4,
          infoHtml: '',
        },
        {
          id: 'f6091900-35c3-493d-82d1-155cb2025724',
          isHidden: false,
          name: 'TypeScript',
          level: 3,
          infoHtml: '',
        },
        {
          id: '68d44051-78df-4dcd-a41f-c5609129b629',
          isHidden: false,
          name: 'C/C++',
          level: 4,
          infoHtml: '',
        },
        {
          id: '0db67f9b-adb9-4eb1-a352-2e2a9005cab5',
          isHidden: false,
          name: 'SQL',
          level: 3,
          infoHtml: '',
        },
        {
          id: '7ace5184-a0b1-40d7-8b20-b6434645b670',
          isHidden: false,
          name: 'PostgreSQL',
          level: 3,
          infoHtml: '',
        },
        {
          id: 'e6dc902c-4bf0-419c-b0c1-ff7e3a12ba12',
          isHidden: false,
          name: 'Docker',
          level: 3,
          infoHtml: '',
        },
        {
          id: '7ae6447a-1f6c-43d7-bc1c-e2d5faa84609',
          isHidden: false,
          name: 'AWS',
          level: 3,
          infoHtml: '',
        },
        {
          id: '4f4571ca-d8ab-445d-9653-aa8eb4a66d1b',
          isHidden: false,
          name: 'OpenTofu',
          level: 3,
          infoHtml: '',
        },
        {
          id: 'cac3626d-f668-4bd0-872d-34b73455374e',
          isHidden: false,
          name: 'GitHub Actions',
          level: 3,
          infoHtml: '',
        },
        {
          id: '202157a7-cdde-48da-ba48-e0bb1a44c164',
          isHidden: false,
          name: 'MCP',
          level: 4,
          infoHtml: '',
        },
      ],
    },
    project: {
      sectionType: 'project',
      displayName: 'Projects',
      iconKey: 'folder',
      entries: [
        {
          id: '8d7df748-a4a2-4446-85b8-75053ff812b6',
          isHidden: false,
          title: 'aboutme.vn',
          subtitle: 'Go, Nuxt, PostgreSQL, AWS',
          link: 'https://aboutme.vn',
          description:
            '<p>Resume builder that renders one resume as a web page and '
            + 'an A4 PDF.</p>',
        },
        {
          id: '01b53fe7-a899-44af-9975-6440ea003404',
          isHidden: false,
          title: 'secops',
          subtitle: 'Go',
          link: '',
          description:
            '<p>CLI and SDK that manages Google SecOps SIEM and SOAR as '
            + 'code: pull, diff, and push, with a built-in MCP server.</p>',
        },
        {
          id: 'f8d25e1c-71f8-4d3b-b1bc-3af2ad261e52',
          isHidden: false,
          title: 'banhmi',
          subtitle: 'Go',
          link: '',
          description:
            '<p>MCP server that gives LLMs Vietnamese banking regulation '
            + 'with exact citations to official sources.</p>',
        },
      ],
    },
  },
  customization: {
    font: {
      family: 'be-vietnam-pro',
      baseSizePx: 15,
    },
    colors: {
      primary: '#0f172a',
      text: '#1a1a1a',
      background: '#ffffff',
      accent: '#2563eb',
      surface: '#f1f5f9',
    },
    spacing: {
      sectionGap: 20,
      entryGap: 10,
      lineHeight: 1.5,
      pageMargin: {
        x: 18,
        y: 12,
      },
    },
    heading: {
      style: 'uppercase',
      showRule: true,
    },
    header: {
      align: 'center',
      detailsLayout: 'inline',
      iconStyle: 'outline',
      photoPosition: 'right',
    },
    layout: {
      columns: 2,
      surfaceTarget: 'sidebar',
      sections: {
        main: ['profile', 'work'],
        sidebar: ['skill', 'project', 'education'],
      },
    },
    sectionDisplay: {
      skill: {
        style: 'tag',
      },
      language: {
        style: 'dots',
      },
    },
    pageFormat: 'a4',
    dateFormat: 'Mon YYYY',
  },
};

export const sampleLink = '/danny';
