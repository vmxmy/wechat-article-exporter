import { dataTableConfig } from '@/config/data-table';

export interface FilterItemSchema {
  id: string;
  value: string | string[];
  variant: (typeof dataTableConfig.filterVariants)[number];
  operator: (typeof dataTableConfig.operators)[number];
  filterId: string;
}
