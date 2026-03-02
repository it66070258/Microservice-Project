CREATE TABLE IF NOT EXISTS "Student" (
	"student_id" INTEGER NOT NULL UNIQUE,
	"first_name" VARCHAR(255) NOT NULL,
	"last_name" VARCHAR(255) NOT NULL,
	"email" VARCHAR(255) NOT NULL,
	"password" VARCHAR(255) NOT NULL,
	"birthdate" VARCHAR(255) NOT NULL,
	"gender" VARCHAR(255) NOT NULL,
	"year_level" INTEGER NOT NULL,
	"graded_subject" VARCHAR(255) ARRAY,
	PRIMARY KEY("student_id")
);