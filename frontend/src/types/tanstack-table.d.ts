import '@tanstack/react-table';

declare module '@tanstack/react-table' {
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    interface ColumnMeta<TData, TValue> {
        cellClassName?: string;
        /** Label in the column-visibility and search-in menus; without it they show the raw `column.id`. */
        columnMenuLabel?: string;
        headerClassName?: string;
        preventRowClick?: boolean;
        /** When true, the column participates in the multi-column search picker (used when DataTable.filterColumn is omitted). */
        searchable?: boolean;
    }
}
