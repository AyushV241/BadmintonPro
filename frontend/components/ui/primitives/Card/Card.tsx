"use client";

import MuiCard from "@mui/material/Card";
import CardActionArea from "@mui/material/CardActionArea";
import CardActions from "@mui/material/CardActions";
import CardContent from "@mui/material/CardContent";
import CardHeader from "@mui/material/CardHeader";
import MuiCardMedia from "@mui/material/CardMedia";
import { testIdAttr } from "../../shared/dom";
import type { CardProps } from "./Card.types";

// MUI spacing units (8px).
const PADDING = { none: 0, sm: 1.5, md: 2.5 } as const;

export function Card({
  children,
  title,
  subtitle,
  headerAction,
  media,
  actions,
  variant = "outlined",
  padding = "md",
  onClick,
  href,
  className,
  id,
  testId,
}: CardProps) {
  const p = PADDING[padding];

  const body = (
    <>
      {media && (
        <MuiCardMedia
          component="img"
          image={media.src}
          alt={media.alt}
          sx={{ height: media.height ?? 160, objectFit: "cover" }}
        />
      )}
      {(title || subtitle || headerAction) && (
        <CardHeader
          title={title}
          subheader={subtitle}
          action={headerAction}
          sx={{ p, pb: children ? 0 : p }}
          slotProps={{
            title: { variant: "h6", component: "h3" },
            subheader: { variant: "body2" },
          }}
        />
      )}
      {children && (
        <CardContent sx={{ p, "&:last-child": { pb: p } }}>{children}</CardContent>
      )}
    </>
  );

  const interactive = Boolean(onClick || href);

  return (
    <MuiCard
      variant={variant === "outlined" ? "outlined" : "elevation"}
      elevation={variant === "elevated" ? 2 : 0}
      className={className}
      id={id}
      sx={{ borderRadius: 3 }}
      {...testIdAttr(testId)}
    >
      {interactive ? (
        href ? (
          <CardActionArea href={href}>{body}</CardActionArea>
        ) : (
          <CardActionArea onClick={onClick}>{body}</CardActionArea>
        )
      ) : (
        body
      )}
      {actions && <CardActions sx={{ px: p, pb: p, pt: 0, gap: 1 }}>{actions}</CardActions>}
    </MuiCard>
  );
}
