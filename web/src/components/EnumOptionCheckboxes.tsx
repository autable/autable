import { OPTION_SEPARATOR, selectedOptions } from "../tableGrid";
import { Checkbox } from "../ui";

type EnumOptionCheckboxesProps = {
  label: string;
  options: string[];
  value: string;
  disabled?: boolean;
  onChange: (value: string) => void;
};

// A multi-valued enum reads as a set of choices, so it is edited as one.
// The result is joined in the declared option order, so the same selection
// always produces the same stored cell no matter what order it was clicked in.
export function EnumOptionCheckboxes({ label, options, value, disabled, onChange }: EnumOptionCheckboxesProps) {
  const selected = new Set(selectedOptions(value));
  return (
    <div className="flex flex-wrap gap-x-4 gap-y-1" role="group" aria-label={label}>
      {options.map((option) => (
        <Checkbox
          key={option}
          checked={selected.has(option)}
          disabled={disabled}
          label={option}
          onChange={(event) => {
            const next = new Set(selected);
            if (event.target.checked) {
              next.add(option);
            } else {
              next.delete(option);
            }
            onChange(options.filter((candidate) => next.has(candidate)).join(OPTION_SEPARATOR));
          }}
        />
      ))}
    </div>
  );
}
