// The gate's one inline <style>, in literal token values
// (docs/design/viewer-analytics/sign-in-to-view.md, "Gate page"). It holds
// no url(, @import, or expression(, so it passes the same unsafe-inline-style
// rule Go applies to style attributes.
export const GATE_STYLE = [
  'body{margin:0;background:#F5F8FF;',
  'font-family:"Be Vietnam Pro",Inter,system-ui,sans-serif;}',
  '#public-gate{display:flex;justify-content:center;',
  'padding:32px 16px 16px;box-sizing:border-box;}',
  '.gate-card{width:100%;max-width:480px;background:#FFFFFF;',
  'border:1px solid #DCE5F5;border-radius:14px;padding:24px;',
  'box-sizing:border-box;}',
  '.gate-title{margin:0 0 12px;font-size:20px;line-height:1.3;',
  'color:#101B3F;}',
  '.gate-text{margin:0;font-size:15px;line-height:1.5;color:#101B3F;}',
  '.gate-providers{display:flex;flex-direction:column;gap:12px;',
  'margin-top:16px;}',
  '.gate-provider{display:flex;align-items:center;justify-content:center;',
  'height:44px;border:1px solid #DCE5F5;border-radius:10px;',
  'background:#FFFFFF;color:#123EDB;font-size:15px;font-weight:500;',
  'text-decoration:none;}',
  '.gate-provider:hover{background:#EAF2FF;}',
  '.gate-provider:focus-visible{outline:2px solid #1A5CEB;',
  'outline-offset:2px;}',
  '.gate-message{margin:12px 0 0;font-size:14px;color:#56648C;}',
  '.gate-refusal{margin:20px 0 0;font-size:14px;color:#56648C;}',
  '.gate-home{display:block;margin-top:16px;font-size:14px;',
  'color:#123EDB;text-decoration:underline;}',
  '@media (min-width:640px){#public-gate{min-height:100vh;',
  'align-items:center;padding:16px;}.gate-card{padding:32px;}}',
].join('');
