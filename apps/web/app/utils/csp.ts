export const HTML_CSP
  = 'default-src \'none\'; base-uri \'none\'; object-src \'none\'; '
    + 'frame-ancestors \'none\'; form-action \'none\'; script-src \'self\'; '
    + 'style-src \'self\' \'unsafe-inline\'; img-src \'self\' data:; '
    + 'font-src \'self\'; connect-src \'self\'; manifest-src \'self\'; '
    + 'media-src \'none\'; worker-src \'none\'';

export const APP_CSP
  = 'default-src \'self\'; base-uri \'self\'; object-src \'none\'; '
    + 'frame-ancestors \'none\'; form-action \'self\'; script-src \'self\'; '
    + 'style-src \'self\' \'unsafe-inline\'; img-src \'self\' data:; '
    + 'font-src \'self\'; connect-src \'self\'; manifest-src \'self\'; '
    + 'media-src \'none\'; worker-src \'self\'';
