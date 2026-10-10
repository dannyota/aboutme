// English text of the Privacy Policy and Terms of Service. Every statement
// here must describe shipped behavior or an owner decision; do not add claims.
import type { LegalCopy } from './legal';

export const legalEn: LegalCopy = {
  updated: 'Last updated October 10, 2026',
  privacyLink: 'Privacy Policy',
  termsLink: 'Terms of Service',
  verifyLink: 'Verify',
  guideLink: 'Connect AI',
  agreement: [
    'By creating an account you confirm you are at least 16 and agree '
    + 'to the ',
    ' and the ',
    '.',
  ],
  privacy: {
    title: 'Privacy Policy',
    description:
      'The aboutme.vn privacy policy: what we collect, what we use it for, '
      + 'the choices you have.',
    intro:
      'aboutme.vn is an open-source resume builder. This page explains '
      + 'what we collect, what we use it for, and the choices you have.',
    operator: {
      text:
        'aboutme.vn is operated by Danny, an individual, on a '
        + 'non-commercial basis in Vietnam.',
      contactLabel: 'Contact',
    },
    sections: [
      {
        heading: 'What we collect',
        items: [
          'Account: your email, your name, and your password, stored only '
          + 'as an Argon2id hash. If you sign in with Google or LinkedIn: '
          + 'your account ID with that provider, your email, and your '
          + 'name.',
          'LinkedIn import: your browser reads the LinkedIn profile PDF '
          + 'you pick. We never receive the file; we store only the '
          + 'resume you create from it.',
          'Content: the resumes you write, the photos you upload, and, '
          + 'while a resume is public, the preview image we make from it '
          + 'for link previews.',
          'Community showcase: if you turn it on for a resume, we store '
          + 'when you turned it on and the role you picked.',
          'Sessions: the browser user-agent and IP address of each '
          + 'session, used for security. Settings → Signed-in devices lists '
          + 'your signed-in devices. We delete the IP address and browser '
          + '(user agent) recorded for a sign-in no later than 90 days '
          + 'after that sign-in. Staying signed in does not extend this.',
          'Second factor: your passkey public keys, your authenticator-'
          + 'app secret (encrypted), and hashes of your recovery codes. '
          + 'Passkey counter anomalies are kept for 180 days.',
          'Connected agents: the app name, its redirect addresses, the '
          + 'access you granted, and when it was last used. Access '
          + 'tokens are stored only as hashes.',
          'Ordinary server operational logs do not record request IP '
          + 'addresses or request headers and are kept for up to 30 days.',
          'Attack protection: CrowdSec receives a five-field request feed '
          + 'with the UTC timestamp, source IP address, HTTP method, response '
          + 'status, and broad route class. The feed does not record the '
          + 'specific path or URI, query string, request or response '
          + 'headers, or request or response body. The feed stays only in '
          + 'RAM. CrowdSec attack alerts and bans are stored on the '
          + 'encrypted Vietnam data volume. We keep the feed and all '
          + 'CrowdSec attack records, including HTTP and SSH alerts and '
          + 'bans, for at most 24 hours. None of this data is shared with '
          + 'the CrowdSec community.',
          'Password check: when you set a password, we check whether it '
          + 'has appeared in a known breach using Have I Been Pwned. Only '
          + 'the first 5 characters of its SHA-1 hash are sent, never the '
          + 'password.',
          'Views of public resumes: we count views and keep only daily '
          + 'totals. To tell people from bots, your IP address and '
          + 'browser details are used only in server memory and '
          + 'discarded the same day, and your browser solves a small '
          + 'computing task. Counting uses no cookie.',
          'If a resume owner requires sign-in to view: Google or '
          + 'LinkedIn confirms your account. We ask only for sign-in, not '
          + 'your name or email, so we receive only an account ID from the '
          + 'provider, and we discard it during that sign-in. The owner is '
          + 'not told who you are.',
        ],
      },
      {
        heading: 'Why we use your data',
        paragraphs: [
          'We process your personal data to perform our agreement with '
          + 'you under the Terms of Service: to provide your account and '
          + 'the resumes you create, keep the service secure, and send '
          + 'account emails. Optional features (publishing, search and '
          + 'AI indexing, the community showcase, connected AI agents, '
          + 'Google or LinkedIn sign-in) run only when you turn them on, '
          + 'and you can turn them off at any time. You can end the '
          + 'agreement by deleting your account.',
          'Viewers of public resumes need no account. When you open a '
          + 'public resume, we use your IP address and browser details to '
          + 'serve the page, stop abuse, and keep bots out of the view '
          + 'count. The result is only a daily total that describes no '
          + 'one. If the owner requires sign-in to view, you choose to '
          + 'sign in to ask to view that resume; we process the account '
          + 'ID only to answer that request.',
        ],
      },
      {
        heading: 'What we don\'t do',
        items: [
          'No ads.',
          'No third-party analytics or advertising trackers. We count '
          + 'views of public resumes only as described above.',
          'We don\'t sell your data or share it for marketing.',
          'Cookies are used only to keep you signed in, to complete '
          + 'Google or LinkedIn sign-in, to hold a pending second-factor '
          + 'sign-in for five minutes, to remember your theme and '
          + 'language, and, when a resume owner requires sign-in to '
          + 'view, to let you view that resume for 7 days.',
          'Browser storage is used only to remember: the page to return '
          + 'to after you verify your email (for up to 24 hours); the '
          + 'preview mode (PDF or web), zoom, and editor panel width you '
          + 'chose in the resume editor; whether you closed the '
          + 'free-resume invite (for up to 90 days); and, for the current '
          + 'browser session only (sessionStorage), when a public resume '
          + 'page last reloaded itself, to stop reload loops. None of '
          + 'this is sent to our servers.',
        ],
      },
      {
        heading: 'Public only by your choice',
        paragraphs: [
          'A resume is private until you publish it. Each resume has its '
          + 'own link. Search engine and AI indexing stays off until you '
          + 'turn it on. When you unpublish, the public link stops '
          + 'working right away. When you delete or rename a resume, we '
          + 'keep its old web address reserved for 180 days so no one '
          + 'else can take over your link. The reservation is not linked '
          + 'to your account, and we delete it after the 180 days.',
          'Public pages are delivered directly from our host in Vietnam. '
          + 'While a resume is public, anyone who can see it can save or '
          + 'screenshot it. By default, viewers can also download the '
          + 'resume\'s PDF; you can turn this off. Avoid putting sensitive '
          + 'personal data, such as ID numbers or ID card images, health '
          + 'information, religion, or political views, in a public resume.',
          'When anyone shares your public link in a chat app or social '
          + 'network, that service fetches the page\'s title, summary, and '
          + 'preview image (your name, headline, and photo), and may keep '
          + 'its own copy after you unpublish.',
          'The community showcase (aboutme.vn/showcase) lists only '
          + 'resumes whose owners turn on Show in the community '
          + 'showcase. It shows the resume\'s preview image (your name, '
          + 'headline, and photo), its template, language, and the role '
          + 'you picked, with a link to the resume. When you turn the option '
          + 'off, unpublish, or turn on Require sign-in to view, the resume '
          + 'leaves the showcase right away. Search engines are asked not '
          + 'to index the showcase, but anyone who visits it can see and '
          + 'copy what it shows.',
        ],
      },
      {
        heading: 'Where your data is stored',
        paragraphs: [
          'Your account, resumes, photos, backups, and logs are stored on '
          + 'GreenNode infrastructure in Ho Chi Minh City, Vietnam. Bizfly '
          + 'Email Transaction sends account emails over SMTP. Amazon '
          + 'Route 53 provides DNS.',
          'Google (United States) and LinkedIn (United States) provide '
          + 'optional sign-in. When you use one to sign in to your account, '
          + 'you sign in at that provider and we receive your name, email, '
          + 'and account ID. Emails you send us are kept in a Google '
          + 'Workspace mailbox.',
          'Google or LinkedIn sign-in, support email, connected AI services '
          + 'you choose, and foreign services that fetch a public resume '
          + 'link may involve processing outside Vietnam. Password checks '
          + 'use Have I Been Pwned, which receives only the first 5 '
          + 'characters of the password\'s SHA-1 hash.',
        ],
      },
      {
        heading: 'Connected AI agents',
        paragraphs: [
          'An AI agent you connect can do only what you allow: read your '
          + 'resumes and, with write access, create, edit, or delete '
          + 'resumes and photos. It cannot publish or unpublish, but '
          + 'deleting a published resume takes its link down. Content the '
          + 'agent reads goes to the AI service you chose, which may be '
          + 'outside Vietnam. You decide this transfer. You can revoke '
          + 'access at any time in Settings.',
        ],
      },
      {
        heading: 'Your controls',
        paragraphs: [
          'You can export your account data, edit or delete your '
          + 'resumes, and delete your account. We keep your account, '
          + 'resumes, and photos until you delete them or your account. '
          + 'The export holds your '
          + 'profile and resume content; email us for photos or session '
          + 'history.',
          'When you delete your account:',
        ],
        items: [
          'Access is removed immediately.',
          'Uploaded photos are deleted, normally within 24 hours.',
          'Database backups keep earlier copies for up to 30 days after '
          + 'that; they are used only for disaster recovery.',
          'Records of account deletions and provider unlinks, holding only '
          + 'the event type and time, are kept for up to 180 days.',
          'If we delete an account for a breach of our Terms of Service, we '
          + 'keep a record of the account ID, the web addresses of its '
          + 'resumes, and the time, for up to 180 days.',
        ],
      },
      {
        heading: 'Emails',
        paragraphs: [
          'We send only account emails: email verification, password '
          + 'reset, and security notices when your password, second '
          + 'factor, passkeys, authenticator app, or recovery codes '
          + 'change. No marketing.',
        ],
      },
      {
        heading: 'Your rights',
        paragraphs: [
          'You have the right to know how your personal data is '
          + 'processed; to consent, refuse, or withdraw consent; to view, '
          + 'correct, or ask us to correct it; to ask for a copy, '
          + 'deletion, or restricted processing, and to object to '
          + 'processing; to ask us to take measures to protect your data; '
          + 'and to complain, report, sue, and claim damages as the law '
          + 'allows. You can export your data, edit or delete resumes, '
          + 'and delete your account yourself in Settings. Send other '
          + 'requests to the address below; if a request concerns an '
          + 'account, send it from that account\'s email address so we '
          + 'can verify it. We reply within 2 working days. We let you '
          + 'view, correct, or get a copy of your data within 10 days; '
          + 'delete data within 20 days; and handle requests to restrict, '
          + 'object, withdraw consent, or take protective measures within '
          + '15 days. If a request needs more time, we tell you why and '
          + 'extend only once, within the limits the law allows.',
          'If an incident exposes or loses your personal data, we report '
          + 'it to the data protection authority of the Ministry of '
          + 'Public Security as the law requires, and we email you.',
        ],
      },
      {
        heading: 'Changes and contact',
        paragraphs: [
          'When this policy changes, we update this page and its date. '
          + 'If we change why we process data or what we collect, we '
          + 'will email you before the change applies.',
        ],
        contactLabel: 'Send questions and requests to',
      },
    ],
  },
  terms: {
    title: 'Terms of Service',
    description:
      'The terms for using aboutme.vn, a free and open-source resume '
      + 'builder.',
    intro: 'These terms apply when you use aboutme.vn.',
    sections: [
      {
        heading: 'The service',
        paragraphs: [
          'aboutme.vn is free. Its code is open source under the '
          + 'AGPL-3.0 license.',
        ],
        repositoryLink: 'Source code on GitHub',
      },
      {
        heading: 'Your content',
        paragraphs: [
          'You own the content you create. You give aboutme.vn permission '
          + 'to store, back up, render as PDF, send to AI agents you '
          + 'connect, and display your content the way you choose, for '
          + 'example by publishing a resume. This permission ends when '
          + 'you delete the content, except for backup copies until they '
          + 'expire.',
          'If you turn on Show in the community showcase for a resume, '
          + 'you also let aboutme.vn show its preview image, template, '
          + 'language, and the role you picked on the showcase page, '
          + 'until you turn it off or unpublish.',
          'You are responsible for what you publish, including its '
          + 'accuracy and your right to share other people\'s '
          + 'information or images.',
        ],
      },
      {
        heading: 'Acceptable use',
        paragraphs: [
          'Do not use aboutme.vn to:',
        ],
        items: [
          'post illegal content;',
          'impersonate anyone;',
          'post other people\'s personal data without their permission;',
          'send spam, spread malware, or abuse the service or other people.',
        ],
        after: [
          'We may remove content or delete accounts that break these rules.',
        ],
      },
      {
        heading: 'Your account',
        items: [
          'Keep your password safe.',
          'One person per account.',
          'Up to three resumes per account.',
          'You must be at least 16 years old.',
          'If we learn an account belongs to someone under 16, we will '
          + 'delete it.',
          'You are responsible for what an AI agent you connect does '
          + 'with your account.',
        ],
      },
      {
        heading: 'Termination',
        paragraphs: [
          'You can delete your account at any time. If we remove content '
          + 'or suspend an account for a breach, we will email you and '
          + 'allow reasonable time to export your data, unless the '
          + 'breach is serious or the law requires otherwise.',
        ],
      },
      {
        heading: 'No warranty',
        paragraphs: [
          'The service is provided as is. It may change and may '
          + 'occasionally be unavailable. If we plan to shut it down, we '
          + 'will try to give notice first so you can export your data.',
        ],
      },
      {
        heading: 'Limitation of liability',
        paragraphs: [
          'Our liability is limited to the extent the law allows.',
        ],
      },
      {
        heading: 'Credits',
        paragraphs: [
          'The LinkedIn icon is from Font Awesome Free by Fonticons, Inc., '
          + 'licensed under CC BY 4.0 (creativecommons.org/licenses/by/4.0). '
          + 'The GitHub and X icons are from Simple Icons (CC0). Fonts are '
          + 'licensed under the SIL Open Font License; the full notices are '
          + 'in the source code. Trademarks belong to their owners.',
        ],
      },
      {
        heading: 'Governing law',
        paragraphs: [
          'These terms are governed by the laws of Vietnam.',
          'If a dispute arises, we will first try to resolve it by '
          + 'negotiation; if that fails, it will be resolved under '
          + 'Vietnamese law.',
        ],
      },
      {
        heading: 'Changes and contact',
        paragraphs: [
          'When these terms change, we update this page and its date. If '
          + 'a change is material, we will email you at least 15 days '
          + 'before it applies. If you do not agree, you can export your '
          + 'data and delete your account before that date. If you keep '
          + 'using the service after a change takes effect, you accept '
          + 'the updated terms. If a change needs your consent under the '
          + 'law, we will ask for it separately; continued use does not '
          + 'count as consent.',
        ],
        contactLabel: 'Contact',
      },
    ],
  },
};
