export type PaginationErrorCode
  = | 'pagination_measurement_required'
    | 'invalid_measurement'
    | 'invalid_page_geometry'
    | 'non_normalized_one_column_flow';

export class PaginationError extends Error {
  readonly code: PaginationErrorCode;

  constructor(code: PaginationErrorCode, message: string) {
    super(message);
    this.name = 'PaginationError';
    this.code = code;
  }
}
